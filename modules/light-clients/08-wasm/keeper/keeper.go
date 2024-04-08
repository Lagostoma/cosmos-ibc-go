package keeper

import (
	"bytes"
	"context"
	"encoding/hex"

	wasmvm "github.com/CosmWasm/wasmvm/v2"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/store"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/internal/ibcwasm"
	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/cosmos/ibc-go/v8/modules/core/exported"
)

// Keeper defines the 08-wasm keeper
type Keeper struct {
	// implements gRPC QueryServer interface
	types.QueryServer
	// vm contains an implementation of the WasmEngine that's invoked.
	vm  ibcwasm.WasmEngine
	cdc codec.BinaryCodec

	// state management
	schema       collections.Schema
	checksums    collections.KeySet[[]byte]
	storeService store.KVStoreService

	// handling queries
	// TODO(jim): We had a reason we didn't call this interface QueryHanlder or something? Should we rename it to QueryHandler?
	queryPlugins QueryPlugins

	clientKeeper types.ClientKeeper

	authority string
}

// Codec returns the 08-wasm module's codec.
func (k Keeper) Codec() codec.BinaryCodec {
	return k.cdc
}

// GetAuthority returns the 08-wasm module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger.
func (Keeper) Logger(ctx sdk.Context) log.Logger {
	return moduleLogger(ctx)
}

func moduleLogger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+exported.ModuleName+"-"+types.ModuleName)
}

// GetVM returns the keeper's vm engine.
func (k Keeper) GetVM() ibcwasm.WasmEngine {
	return k.vm
}

// GetChecksums returns the stored checksums.
// TODO(jim): Need a better name here, this + grpc query meth + GetAllChecksums are all too similar.
func (k Keeper) GetChecksums() collections.KeySet[[]byte] {
	return k.checksums
}

// GetQueryPlugins returns the set query plugins.
func (k Keeper) GetQueryPlugins() QueryPlugins {
	return k.queryPlugins
}

// SetQueryPlugins sets the plugins.
func (k *Keeper) SetQueryPlugins(plugins QueryPlugins) {
	k.queryPlugins = plugins
}

func (k Keeper) newQueryHandler(ctx sdk.Context, callerID string) *queryHandler {
	return newQueryHandler(ctx, k.GetQueryPlugins(), callerID)
}

func (k Keeper) storeWasmCode(ctx sdk.Context, code []byte, storeFn func(code wasmvm.WasmCode, gasLimit uint64) (wasmvm.Checksum, uint64, error)) ([]byte, error) {
	var err error
	if types.IsGzip(code) {
		ctx.GasMeter().ConsumeGas(types.VMGasRegister.UncompressCosts(len(code)), "Uncompress gzip bytecode")
		code, err = types.Uncompress(code, types.MaxWasmByteSize())
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to store contract")
		}
	}

	// run the code through the wasm light client validation process
	if err := types.ValidateWasmCode(code); err != nil {
		return nil, errorsmod.Wrap(err, "wasm bytecode validation failed")
	}

	// Check to see if store already has checksum.
	checksum, err := types.CreateChecksum(code)
	if err != nil {
		return nil, errorsmod.Wrap(err, "wasm bytecode checksum failed")
	}

	if k.HasChecksum(ctx, checksum) {
		return nil, types.ErrWasmCodeExists
	}

	// create the code in the vm
	gasLeft := types.VMGasRegister.RuntimeGasForContract(ctx)
	vmChecksum, gasUsed, err := storeFn(code, gasLeft)
	types.VMGasRegister.ConsumeRuntimeGas(ctx, gasUsed)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to store contract")
	}

	// SANITY: We've checked our store, additional safety check to assert that the checksum returned by WasmVM equals checksum generated by us.
	if !bytes.Equal(vmChecksum, checksum) {
		return nil, errorsmod.Wrapf(types.ErrInvalidChecksum, "expected %s, got %s", hex.EncodeToString(checksum), hex.EncodeToString(vmChecksum))
	}

	// pin the code to the vm in-memory cache
	if err := k.GetVM().Pin(vmChecksum); err != nil {
		return nil, errorsmod.Wrapf(err, "failed to pin contract with checksum (%s) to vm cache", hex.EncodeToString(vmChecksum))
	}

	// store the checksum
	err = k.GetChecksums().Set(ctx, checksum)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to store checksum")
	}

	return checksum, nil
}

func (k Keeper) migrateContractCode(ctx sdk.Context, clientID string, newChecksum, migrateMsg []byte) error {
	wasmClientState, err := k.GetWasmClientState(ctx, clientID)
	if err != nil {
		return errorsmod.Wrap(err, "failed to retrieve wasm client state")
	}
	oldChecksum := wasmClientState.Checksum

	clientStore := k.clientKeeper.ClientStore(ctx, clientID)
	clientState, found := types.GetClientState(clientStore, k.cdc)
	if !found {
		return errorsmod.Wrap(clienttypes.ErrClientNotFound, clientID)
	}

	if !k.HasChecksum(ctx, newChecksum) {
		return types.ErrWasmChecksumNotFound
	}

	if bytes.Equal(clientState.Checksum, newChecksum) {
		return errorsmod.Wrapf(types.ErrWasmCodeExists, "new checksum (%s) is the same as current checksum (%s)", hex.EncodeToString(newChecksum), hex.EncodeToString(clientState.Checksum))
	}

	// update the checksum, this needs to be done before the contract migration
	// so that wasmMigrate can call the right code. Note that this is not
	// persisted to the client store.
	clientState.Checksum = newChecksum

	err = WasmMigrate(ctx, k, clientStore, clientState, clientID, migrateMsg)
	if err != nil {
		return err
	}

	// client state may be updated by the contract migration
	wasmClientState, err = k.GetWasmClientState(ctx, clientID)
	if err != nil {
		// note that this also ensures that the updated client state is
		// still a wasm client state
		return errorsmod.Wrap(err, "failed to retrieve the updated wasm client state")
	}

	// update the client state checksum before persisting it
	wasmClientState.Checksum = newChecksum

	k.clientKeeper.SetClientState(ctx, clientID, wasmClientState)

	emitMigrateContractEvent(ctx, clientID, oldChecksum, newChecksum)

	return nil
}

// GetWasmClientState returns the 08-wasm client state for the given client identifier.
func (k Keeper) GetWasmClientState(ctx sdk.Context, clientID string) (*types.ClientState, error) {
	clientState, found := k.clientKeeper.GetClientState(ctx, clientID)
	if !found {
		return nil, errorsmod.Wrapf(clienttypes.ErrClientTypeNotFound, "clientID %s", clientID)
	}

	wasmClientState, ok := clientState.(*types.ClientState)
	if !ok {
		return nil, errorsmod.Wrapf(clienttypes.ErrInvalidClient, "expected type %T, got %T", (*types.ClientState)(nil), wasmClientState)
	}

	return wasmClientState, nil
}

// GetAllChecksums is a helper to get all checksums from the store.
// It returns an empty slice if no checksums are found
func (k Keeper) GetAllChecksums(ctx context.Context) ([]types.Checksum, error) {
	iterator, err := k.GetChecksums().Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}

	keys, err := iterator.Keys()
	if err != nil {
		return nil, err
	}

	checksums := []types.Checksum{}
	for _, key := range keys {
		checksums = append(checksums, key)
	}

	return checksums, nil
}

// HasChecksum returns true if the given checksum exists in the store and
// false otherwise.
func (k Keeper) HasChecksum(ctx context.Context, checksum types.Checksum) bool {
	found, err := k.GetChecksums().Has(ctx, checksum)
	if err != nil {
		return false
	}

	return found
}

// InitializePinnedCodes updates wasmvm to pin to cache all contracts marked as pinned
func (k Keeper) InitializePinnedCodes(ctx sdk.Context) error {
	checksums, err := k.GetAllChecksums(ctx)
	if err != nil {
		return err
	}

	for _, checksum := range checksums {
		if err := k.GetVM().Pin(checksum); err != nil {
			return err
		}
	}
	return nil
}
