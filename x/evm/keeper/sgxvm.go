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
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/evmos/ethermint/x/evm/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmtypes "github.com/tendermint/tendermint/types"
	"math/big"
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

	labels := []metrics.Label{
		telemetry.NewLabel("tx_type", fmt.Sprintf("%d", tx.Type())),
	}
	if tx.To() == nil {
		labels = append(labels, telemetry.NewLabel("execution", "create"))
	} else {
		labels = append(labels, telemetry.NewLabel("execution", "call"))
	}

	response, err := k.ApplySGXVMTransaction(ctx, tx)
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

func (k *Keeper) ApplySGXVMTransaction(ctx sdk.Context, tx *ethtypes.Transaction) (*types.MsgEthereumTxResponse, error) {
	var (
		bloom        *big.Int
		bloomReceipt ethtypes.Bloom
		err          error
	)

	cfg, err := k.EVMConfig(ctx, ctx.BlockHeader().ProposerAddress, k.eip155ChainID)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to load evm config")
	}

	// get the signer according to the chain rules from the config and block height
	signer := ethtypes.MakeSigner(cfg.ChainConfig, big.NewInt(ctx.BlockHeight()))
	msg, err := tx.AsMessage(signer, cfg.BaseFee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to return ethereum transaction as core message")
	}

	txContext, err := createSGXVMConfig(ctx, k, tx)
	if err != nil {
		return nil, err
	}

	// convert `to` field to bytes
	var destination []byte
	if msg.To() != nil {
		destination = msg.To().Bytes()
	}

	// snapshot to contain the tx processing and post processing in same scope
	var commit func()
	tmpCtx := ctx
	if k.hooks != nil {
		// Create a cache context to revert state when tx hooks fails,
		// the cache context is only committed when both tx and hooks executed successfully.
		// Didn't use `Snapshot` because the context stack has exponential complexity on certain operations,
		// thus restricted to be used only inside `ApplyMessage`.
		tmpCtx, commit = ctx.CacheContext()
	}

	connector := Connector{
		Ctx:    tmpCtx,
		Keeper: k,
	}

	res, err := librustgo.HandleTx(
		connector,
		msg.From().Bytes(),
		destination,
		msg.Data(),
		msg.Value().Bytes(),
		msg.Gas(),
		txContext,
	)
	if err != nil {
		return nil, err
	}

	txConfig := k.TxConfig(ctx, tx.Hash())
	logs := SGXVMLogsToEthereum(res.Logs, tx, txConfig, txContext.BlockNumber)

	// Compute block bloom filter
	if len(logs) > 0 {
		bloom = k.GetBlockBloomTransient(ctx)
		bloom.Or(bloom, big.NewInt(0).SetBytes(ethtypes.LogsBloom(logs)))
		bloomReceipt = ethtypes.BytesToBloom(bloom.Bytes())
	}

	cumulativeGasUsed := res.GasUsed
	if ctx.BlockGasMeter() != nil {
		limit := ctx.BlockGasMeter().Limit()
		cumulativeGasUsed += ctx.BlockGasMeter().GasConsumed()
		if cumulativeGasUsed > limit {
			cumulativeGasUsed = limit
		}
	}

	var contractAddr common.Address
	if msg.To() == nil {
		contractAddr = crypto.CreateAddress(msg.From(), msg.Nonce())
	}

	receipt := &ethtypes.Receipt{
		Type:              tx.Type(),
		PostState:         nil, // TODO: intermediate state root
		CumulativeGasUsed: cumulativeGasUsed,
		Bloom:             bloomReceipt,
		Logs:              logs,
		TxHash:            txConfig.TxHash,
		ContractAddress:   contractAddr,
		GasUsed:           res.GasUsed,
		BlockHash:         txConfig.BlockHash,
		BlockNumber:       big.NewInt(ctx.BlockHeight()),
		TransactionIndex:  txConfig.TxIndex,
	}

	if res.VmError != "" {
		receipt.Status = ethtypes.ReceiptStatusSuccessful
		// Only call hooks if tx executed successfully.
		if err = k.PostTxProcessing(tmpCtx, msg, receipt); err != nil {
			// If hooks return error, revert the whole tx.
			res.VmError = types.ErrPostTxProcessing.Error()
			k.Logger(ctx).Error("tx post processing failed", "error", err)

			// If the tx failed in post-processing hooks, we should clear the logs
			res.Logs = nil
		} else if commit != nil {
			// PostTxProcessing is successful, commit the tmpCtx
			commit()
			// Since the post-processing can alter the log, we need to update the result
			//res.Logs = types.NewLogsFromEth(receipt.Logs) // TODO: Handle incompatible types of Ethermint's log and our log
			ctx.EventManager().EmitEvents(tmpCtx.EventManager().Events())
		}
	}

	if len(receipt.Logs) > 0 {
		// Update transient block bloom filter
		k.SetBlockBloomTransient(ctx, receipt.Bloom.Big())
		k.SetLogSizeTransient(ctx, uint64(txConfig.LogIndex)+uint64(len(receipt.Logs)))
	}

	k.SetTxIndexTransient(ctx, uint64(txConfig.TxIndex)+1)

	totalGasUsed, err := k.AddTransientGasUsed(ctx, res.GasUsed)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to add transient gas used")
	}

	// reset the gas meter for current cosmos transaction
	k.ResetGasMeterAndConsumeGas(ctx, totalGasUsed)
	return res, nil
}

func createSGXVMConfig(ctx sdk.Context, k *Keeper, tx *ethtypes.Transaction) (*librustgo.TransactionContext, error) {
	cfg, err := k.EVMConfig(ctx, ctx.BlockHeader().ProposerAddress, k.eip155ChainID)
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

// SGXVMLogsToEthereum converts logs from SGXVM to ethereum format
func SGXVMLogsToEthereum(logs []*librustgo.Log, tx *ethtypes.Transaction, txConfig statedb.TxConfig, blockNumber uint64) []*ethtypes.Log {
	var ethLogs []*ethtypes.Log
	for i := range logs {
		ethLogs = append(ethLogs, SGXVMLogToEthereum(logs[i], tx, txConfig, blockNumber))
	}
	return ethLogs
}

func SGXVMLogToEthereum(log *librustgo.Log, tx *ethtypes.Transaction, txConfig statedb.TxConfig, blockNumber uint64) *ethtypes.Log {
	var topics []common.Hash
	for _, topic := range log.Topics {
		topics = append(topics, common.BytesToHash(topic.Inner))
	}

	return &ethtypes.Log{
		Address:     common.BytesToAddress(log.Address),
		Topics:      topics,
		Data:        log.Data,
		BlockNumber: blockNumber,
		TxHash:      tx.Hash(),
		TxIndex:     txConfig.TxIndex,
		BlockHash:   txConfig.BlockHash,
		Index:       txConfig.LogIndex,
		Removed:     false,
	}
}
