package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
)

// msg types
const (
	TypeMsgRegisterCounterpartyAddress = "registerCounterpartyAddress"
)

// NewMsgRegisterCounterpartyAddress creates a new instance of MsgRegisterCounterpartyAddress
func NewMsgRegisterCounterpartyAddress(address, counterpartyAddress string) *MsgRegisterCounterpartyAddress {
	return &MsgRegisterCounterpartyAddress{
		Address:             address,
		CounterpartyAddress: counterpartyAddress,
	}
}

// ValidateBasic performs a basic check of the MsgRegisterCounterpartyAddress fields
func (msg MsgRegisterCounterpartyAddress) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Address)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to convert msg.Address into sdk.AccAddress")
	}

	if msg.CounterpartyAddress == "" {
		return ErrCounterpartyAddressEmpty
	}

	return nil
}

// GetSigners implements sdk.Msg
func (msg MsgRegisterCounterpartyAddress) GetSigners() []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(msg.Address)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
}

// NewMsgPayPacketFee creates a new instance of MsgPayPacketFee
func NewMsgPayPacketFee(fee Fee, sourcePortId, sourceChannelId, signer string, relayers []string) *MsgPayPacketFee {
	return &MsgPayPacketFee{
		Fee:             fee,
		SourcePortId:    sourcePortId,
		SourceChannelId: sourceChannelId,
		Signer:          signer,
		Relayers:        relayers,
	}
}

// ValidateBasic performs a basic check of the MsgPayPacketFee fields
func (msg MsgPayPacketFee) ValidateBasic() error {
	// validate channelId
	err := host.ChannelIdentifierValidator(msg.SourceChannelId)
	if err != nil {
		return err
	}

	// validate portId
	err = host.PortIdentifierValidator(msg.SourcePortId)
	if err != nil {
		return err
	}

	// signer check
	_, err = sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to convert msg.Signer into sdk.AccAddress")
	}

	// enforce relayer is nil
	if msg.Relayers != nil {
		return ErrRelayersNotNil
	}

	// validate Fee
	if err := msg.Fee.Validate(); err != nil {
		return err
	}

	return nil
}

// GetSigners implements sdk.Msg
func (msg MsgPayPacketFee) GetSigners() []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
}

// NewMsgPayPacketAsync creates a new instance of MsgPayPacketFee
func NewMsgPayPacketFeeAsync(identifiedPacketFee IdentifiedPacketFee) *MsgPayPacketFeeAsync {
	return &MsgPayPacketFeeAsync{
		IdentifiedPacketFee: identifiedPacketFee,
	}
}

// ValidateBasic performs a basic check of the MsgPayPacketFeeAsync fields
func (msg MsgPayPacketFeeAsync) ValidateBasic() error {
	// signer check
	_, err := sdk.AccAddressFromBech32(msg.IdentifiedPacketFee.RefundAddress)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to convert msg.Signer into sdk.AccAddress")
	}

	err = msg.IdentifiedPacketFee.Validate()
	if err != nil {
		return sdkerrors.Wrap(err, "Invalid IdentifiedPacketFee")
	}

	return nil
}

// GetSigners implements sdk.Msg
// The signer of the fee message must be the refund address
func (msg MsgPayPacketFeeAsync) GetSigners() []sdk.AccAddress {
	signer, err := sdk.AccAddressFromBech32(msg.IdentifiedPacketFee.RefundAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{signer}
}

func NewIdentifiedPacketFee(packetId channeltypes.PacketId, fee Fee, refundAddr string, relayers []string) IdentifiedPacketFee {
	return IdentifiedPacketFee{
		PacketId:      packetId,
		Fee:           fee,
		RefundAddress: refundAddr,
		Relayers:      relayers,
	}
}

func (fee IdentifiedPacketFee) Validate() error {
	// validate PacketId
	err := fee.PacketId.Validate()
	if err != nil {
		return sdkerrors.Wrap(err, "invalid PacketId")
	}

	_, err = sdk.AccAddressFromBech32(fee.RefundAddress)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to convert RefundAddress into sdk.AccAddress")
	}

	// enforce relayer is nil
	if fee.Relayers != nil {
		return ErrRelayersNotNil
	}

	// validate Fee
	if err := fee.Fee.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate asserts that each Fee is valid and all three Fees are not empty or zero
func (fee Fee) Validate() error {
	// if any of the fee's are invalid return an error
	if !fee.AckFee.IsValid() || !fee.RecvFee.IsValid() || !fee.TimeoutFee.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "contains one or more invalid fees")
	}

	// if all three fee's are zero or empty return an error
	if fee.AckFee.IsZero() && fee.RecvFee.IsZero() && fee.TimeoutFee.IsZero() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "contains one or more invalid fees")
	}

	return nil
}
