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
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/evmos/ethermint/x/evm/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmtypes "github.com/tendermint/tendermint/types"
	"strconv"
)

// HandleTx receives a transaction which is then
// executed (applied) against the SGX-protected EVM. The provided SDK Context is set to the Keeper
// so that it can implement and call the StateDB methods without receiving it as a function
// parameter.
func (k *Keeper) HandleTx(goCtx context.Context, msg *types.MsgEthereumTx) (*types.MsgEthereumTxResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	tx := msg.AsTransaction()
	txIndex := k.GetTxIndexTransient(ctx)

	sender, decodingError := hexutil.Decode(msg.From)
	if decodingError != nil {
		return nil, decodingError
	}

	labels := []metrics.Label{
		telemetry.NewLabel("tx_type", fmt.Sprintf("%d", tx.Type())),
	}
	if tx.To() == nil {
		labels = append(labels, telemetry.NewLabel("execution", "create"))
	} else {
		labels = append(labels, telemetry.NewLabel("execution", "call"))
	}

	// TODO: librustgo should accept all types of txs, such as LegacyTx, EIP1159, EIP2930 tx.
	// TODO: Need to check it in tests
	execResult, execError := librustgo.HandleTx(
		querier, // TODO: Construct querier
		sender,
		tx.To().Bytes(),
		tx.Data(),
		tx.Value().Bytes(),
		tx.Gas(),
	)
	if execError != nil {
		return nil, errorsmod.Wrap(execError, "failed to apply transaction")
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"tx", "msg", "ethereum_tx", "total"},
			1,
			labels,
		)

		if execResult.GasUsed != 0 {
			telemetry.IncrCounterWithLabels(
				[]string{"tx", "msg", "ethereum_tx", "gas_used", "total"},
				float32(execResult.GasUsed),
				labels,
			)

			// Observe which users define a gas limit >> gas used. Note, that
			// gas_limit and gas_used are always > 0
			gasLimit := sdk.NewDec(int64(tx.Gas()))
			gasRatio, err := gasLimit.QuoInt64(int64(execResult.GasUsed)).Float64()
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
		sdk.NewAttribute(types.AttributeKeyTxGasUsed, strconv.FormatUint(execResult.GasUsed, 10)),
	}

	if len(ctx.TxBytes()) > 0 {
		// add event for tendermint transaction hash format
		hash := tmbytes.HexBytes(tmtypes.Tx(ctx.TxBytes()).Hash())
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyTxHash, hash.String()))
	}

	if to := tx.To(); to != nil {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyRecipient, to.Hex()))
	}

	if execResult.VmError != "" {
		attrs = append(attrs, sdk.NewAttribute(types.AttributeKeyEthereumTxFailed, execResult.VmError))
	}

	txLogAttrs := make([]sdk.Attribute, len(execResult.Logs))
	for i, log := range execResult.Logs {
		value, err := json.Marshal(log)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to encode log")
		}
		txLogAttrs[i] = sdk.NewAttribute(types.AttributeKeyTxLog, string(value))
	}

	// emit events
	// TODO: Move to EmitTypedEvents
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

	logs := make([]*types.Log, len(execResult.Logs))
	for i, log := range execResult.Logs {
		protoLog := &types.Log{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    log.Data,
		}
		logs[i] = protoLog
	}

	response := &types.MsgEthereumTxResponse{
		Hash:    tx.Hash().String(), // TODO: Maybe we should cache tx.Hash somewhere
		Logs:    logs,
		Ret:     execResult.Ret,
		VmError: execResult.VmError,
		GasUsed: execResult.GasUsed,
	}
	return response, nil
}
