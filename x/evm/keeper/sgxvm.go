package keeper

import (
	"context"
	errorsmod "cosmossdk.io/errors"
	"encoding/json"
	"fmt"
	"github.com/SigmaGmbH/librustgo"
	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmtypes "github.com/tendermint/tendermint/types"
	"strconv"
)

// HandleTx receives a transaction which is then
// executed (applied) against the SGX-protected EVM. The provided SDK Context is set to the Keeper
// so that it can implement and call the StateDB methods without receiving it as a function
// parameter.
func (k *Keeper) HandleTx(goCtx context.Context, msg *types.MsgHandleTx) (*types.MsgEthereumTxResponse, error) {
	var (
		err error
	)

	ctx := sdk.UnwrapSDKContext(goCtx)
	tx := msg.AsTransaction()
	txIndex := k.GetTxIndexTransient(ctx)

	sender, err := hexutil.Decode(msg.From)
	if err != nil {
		return nil, errorsmod.Wrap(err, "cannot decode transaction sender")
	}

	labels := []metrics.Label{
		telemetry.NewLabel("tx_type", fmt.Sprintf("%d", tx.Type())),
	}
	if tx.To() == nil {
		labels = append(labels, telemetry.NewLabel("execution", "create"))
	} else {
		labels = append(labels, telemetry.NewLabel("execution", "call"))
	}

	response, err := k.ApplySGXVMTransaction(ctx, tx, sender)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to apply transaction")
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"tx", "msg", "ethereum_tx", "total"},
			1,
			labels,
		)

		if response.GasUsed != 0 {
			telemetry.IncrCounterWithLabels(
				[]string{"tx", "msg", "ethereum_tx", "gas_used", "total"},
				float32(response.GasUsed),
				labels,
			)

			// Observe which users define a gas limit >> gas used. Note, that
			// gas_limit and gas_used are always > 0
			gasLimit := sdk.NewDec(int64(tx.Gas()))
			gasRatio, err := gasLimit.QuoInt64(int64(response.GasUsed)).Float64()
			if err == nil {
				telemetry.SetGaugeWithLabels(
					[]string{"tx", "msg", "ethereum_tx", "gas_limit", "per", "gas_used"},
					float32(gasRatio),
					labels,
				)
			}
		}
	}()

	attrs := []sdk.Attribute{
		sdk.NewAttribute(sdk.AttributeKeyAmount, tx.Value().String()),
		// add event for ethereum transaction hash format
		sdk.NewAttribute(types.AttributeKeyEthereumTxHash, tx.Hash().String()),
		// add event for index of valid ethereum tx
		sdk.NewAttribute(types.AttributeKeyTxIndex, strconv.FormatUint(txIndex, 10)),
		// add event for eth tx gas used, we can't get it from cosmos tx result when it contains multiple eth tx msgs.
		sdk.NewAttribute(types.AttributeKeyTxGasUsed, strconv.FormatUint(response.GasUsed, 10)),
	}

	if len(ctx.TxBytes()) > 0 {
		// add event for tendermint transaction hash format
		hash := tmbytes.HexBytes(tmtypes.Tx(ctx.TxBytes()).Hash())
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyTxHash, hash.String()))
	}

	if to := tx.To(); to != nil {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyRecipient, to.Hex()))
	}

	if response.Failed() {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyEthereumTxFailed, response.VmError))
	}

	txLogAttrs := make([]sdk.Attribute, len(response.Logs))
	for i, log := range response.Logs {
		value, err := json.Marshal(log)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to encode log")
		}
		txLogAttrs[i] = sdk.NewAttribute(types.AttributeKeyTxLog, string(value))
	}

	// emit events
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeEthereumTx,
			attrs...,
		),
		sdk.NewEvent(
			types.EventTypeTxLog,
			txLogAttrs...,
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
			sdk.NewAttribute(sdk.AttributeKeySender, msg.From),
			sdk.NewAttribute(types.AttributeKeyTxType, fmt.Sprintf("%d", tx.Type())),
		),
	})

	logs := make([]*types.Log, len(response.Logs))
	for i, log := range response.Logs {
		protoLog := &types.Log{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    log.Data,
		}
		logs[i] = protoLog
	}

	return response, nil
}

func (k *Keeper) ApplySGXVMTransaction(ctx sdk.Context, tx *ethtypes.Transaction, sender []byte) (*types.MsgEthereumTxResponse, error) {
	var (
		err error
	)

	// TODO: Copy implementation from ApplyTransaction

	// Convert `to` field to bytes
	var destination []byte
	if tx.To() != nil {
		destination = tx.To().Bytes()
	}

	connector := Connector{
		Ctx:    ctx,
		Keeper: k,
	}

	txContext, err := createSGXVMConfig(ctx, k, tx)
	if err != nil {
		return nil, err
	}

	response, err := librustgo.HandleTx(
		connector,
		sender,
		destination,
		tx.Data(),
		tx.Value().Bytes(),
		tx.Gas(),
		txContext,
	)
	if err != nil {
		return nil, err
	}
}

func logTopicsToStringArray(topics []*librustgo.Topic) []string {
	var stringTopics []string
	for _, topic := range topics {
		stringTopics = append(stringTopics, common.BytesToHash(topic.Inner).String())
	}
	return stringTopics
}

func createSGXVMConfig(ctx sdk.Context, k *Keeper, tx *ethtypes.Transaction) (*librustgo.TransactionContext, error) {
	cfg, err := k.EVMConfig(ctx, sdk.ConsAddress(ctx.BlockHeader().ProposerAddress), k.eip155ChainID)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to load evm config")
	}

	return &librustgo.TransactionContext{
		BlockCoinbase:      cfg.CoinBase.Bytes(),
		BlockNumber:        uint64(ctx.BlockHeight()),
		BlockBaseFeePerGas: cfg.BaseFee.Bytes(),
		Timestamp:          uint64(ctx.BlockHeader().Time.Unix()),
		BlockGasLimit:      ethermint.BlockGasLimit(ctx),
		ChainId:            k.eip155ChainID.Uint64(),
		GasPrice:           tx.GasPrice().Bytes(),
		BlockHash:          common.Hash{}.Bytes(), // TODO: Decide if we need this field
	}, nil
}
