package tendermint

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	clienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	host "github.com/cosmos/ibc-go/v6/modules/core/24-host"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
)

// PruneTendermintConsensusStates prunes all expired tendermint consensus states. This function
// may optionally be called during in-place store migrations. The ibc store key must be provided.
func PruneTendermintConsensusStates(ctx sdk.Context, clientKeeper ClientKeeper) error {
	var clientIDs []string
	k.IterateClientStates(ctx, nil, func(clientID string, _ exported.ClientState) bool {
		clientIDs = append(clientIDs, clientID)
		return false
	})

	// keep track of the total consensus states pruned so chains can
	// understand how much space is saved when the migration is run
	var totalPruned int

	for _, clientID := range clientIDs {
		clientState, ok := clientKeeper.GetClientState(ctx, clientID)
		if !ok {
			return sdkerrors.Wrapf(clienttypes.ErrClientNotFound, "clientID %s", clientID)
		}

		tmClientState, ok := clientState.(*ClientState)
		if !ok {
			return sdkerrors.Wrap(clienttypes.ErrInvalidClient, "client state is not tendermint even though client id contains 07-tendermint")
		}

		totalPruned += PruneAllExpiredConsensusStates(ctx, clientStore, cdc, tmClientState)
	}

	clientLogger := ctx.Logger().With("module", "x/"+host.ModuleName+"/"+clienttypes.SubModuleName)
	clientLogger.Info("pruned expired tendermint consensus states", "total", totalPruned)

	return nil
}
