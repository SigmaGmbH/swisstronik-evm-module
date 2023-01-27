package keeper

import (
	"context"
	errorsmod "cosmossdk.io/errors"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SigmaGmbH/librustgo"
	"github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/types"
	"github.com/golang/protobuf/proto"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmtypes "github.com/tendermint/tendermint/types"
	"math/big"
	"strconv"
)

// Connector allows our VM interact with existing Cosmos application.
// It is passed by pointer into SGX to make it accessible for our VM.
type Connector struct {
	Ctx    sdk.Context
	Keeper Keeper
}

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
	connector := Connector{
		Ctx:    ctx,
		Keeper: *k, // TODO: Check how to avoid moving keeper
	}
	execResult, execError := librustgo.HandleTx(
		connector,
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

func (q Connector) Query(req []byte) ([]byte, error) {
	// Decode protobuf
	decodedRequest := &librustgo.CosmosRequest{}
	if err := proto.Unmarshal(req, decodedRequest); err != nil {
		return nil, err
	}

	switch request := decodedRequest.Req.(type) {
	// Handle request for account data such as balance and nonce
	case *librustgo.CosmosRequest_GetAccount:
		return q.GetAccount(request)
	// Handles request for updating account data
	case *librustgo.CosmosRequest_InsertAccount:
		// TODO: Implement
		return nil, nil
	// Handles request if such account exists
	case *librustgo.CosmosRequest_ContainsKey:
		return q.IsAccountExists(request)
	// Handles contract code request
	case *librustgo.CosmosRequest_AccountCode:
		return q.GetAccountCode(request)
	// Handles storage cell data request
	case *librustgo.CosmosRequest_StorageCell:
		return q.GetStorageCell(request)
	// Handles inserting storage cell
	case *librustgo.CosmosRequest_InsertStorageCell:
		return q.InsertStorageCell(request)
	// Handles updating contract code
	case *librustgo.CosmosRequest_InsertAccountCode:
		return q.InsertAccountCode(request)
	// Handles remove storage cell request
	case *librustgo.CosmosRequest_RemoveStorageCell:
		return q.RemoveStorageCell(request)
	// Handles removing account storage, account record, etc.
	case *librustgo.CosmosRequest_Remove:
		return q.Remove(request)
	// Returns block hash
	case *librustgo.CosmosRequest_BlockHash:
		return q.BlockHash(request)
	// Returns block timestamp
	case *librustgo.CosmosRequest_BlockTimestamp:
		return q.BlockTimestamp(request)
	// Returns block number
	case *librustgo.CosmosRequest_BlockNumber:
		return q.BlockNumber(request)
	// Returns chain id
	case *librustgo.CosmosRequest_ChainId:
		return q.ChainId(request)
	}

	return nil, errors.New("wrong query received")
}

// GetAccount handles incoming protobuf-encoded request for account data such as balance and nonce.
// Returns data in protobuf-encoded format
func (q Connector) GetAccount(req *librustgo.CosmosRequest_GetAccount) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query GetAccount invoked")
	address := common.BytesToAddress(req.GetAccount.Address)
	account := q.Keeper.GetAccount(q.Ctx, address)

	if account == nil {
		// If there is no such account return zero balance and nonce
		return proto.Marshal(&librustgo.QueryGetAccountResponse{
			Balance: make([]byte, 0),
			Nonce:   make([]byte, 0),
		})
	}

	return proto.Marshal(&librustgo.QueryGetAccountResponse{
		Balance: account.Balance.Bytes(),
		Nonce:   sdk.Uint64ToBigEndian(account.Nonce),
	})
}

// InsertAccount handles incoming protobuf-encoded request for adding or modifying existing account data.
func (q Connector) InsertAccount(req *librustgo.CosmosRequest_InsertAccount) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query InsertAccount invoked")
	//address := common.BytesToAddress(req.InsertAccount.Address)
	return nil, nil
}

// InsertAccountCode handles incoming protobuf-encoded request for adding or modifying existing account code
// It will insert account code only if account exists, otherwise it returns an error
func (q Connector) InsertAccountCode(req *librustgo.CosmosRequest_InsertAccountCode) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query InsertAccountCode invoked")

	// Calculate code hash
	codeHash := crypto.Keccak256(req.InsertAccountCode.Code)
	q.Keeper.SetCode(q.Ctx, codeHash, req.InsertAccountCode.Code)

	// Link address with code hash
	cosmosAddr := sdk.AccAddress(req.InsertAccountCode.Address)
	cosmosAccount := q.Keeper.accountKeeper.GetAccount(q.Ctx, cosmosAddr)
	ethAccount := cosmosAccount.(ethermint.EthAccountI)

	// Set code hash if account exists
	if ethAccount != nil {
		if err := ethAccount.SetCodeHash(common.BytesToHash(codeHash)); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("cannot insert account code. Account does not exist")
	}

	return proto.Marshal(&librustgo.QueryInsertAccountCodeResponse{})
}

// RemoveStorageCell handles incoming protobuf-encoded request for removing contract storage cell for given key (index)
func (q Connector) RemoveStorageCell(req *librustgo.CosmosRequest_RemoveStorageCell) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query RemoveStorageCell invoked")
	address := common.BytesToAddress(req.RemoveStorageCell.Address)
	index := common.BytesToHash(req.RemoveStorageCell.Index)

	q.Keeper.SetState(q.Ctx, address, index, nil)

	return proto.Marshal(&librustgo.QueryRemoveStorageCellResponse{})
}

// Remove handles incoming protobuf-encoded request for removing smart contract (selfdestruct)
func (q Connector) Remove(req *librustgo.CosmosRequest_Remove) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query Remove invoked")
	address := common.BytesToAddress(req.Remove.Address)
	err := q.Keeper.DeleteAccount(q.Ctx, address)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove account")
	}
	return proto.Marshal(&librustgo.QueryRemoveResponse{})
}

// BlockHash handles incoming protobuf-encoded request for getting block hash
func (q Connector) BlockHash(req *librustgo.CosmosRequest_BlockHash) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query BlockHash invoked")
	h := q.Ctx.HeaderHash()
	return proto.Marshal(&librustgo.QueryBlockHashResponse{Hash: h.Bytes()})
}

// BlockTimestamp handles incoming protobuf-encoded request for getting last block timestamp
func (q Connector) BlockTimestamp(req *librustgo.CosmosRequest_BlockTimestamp) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query BlockTimestamp invoked")
	t := big.NewInt(q.Ctx.BlockTime().Unix())
	return proto.Marshal(&librustgo.QueryBlockTimestampResponse{Timestamp: t.Bytes()})
}

// BlockNumber handles incoming protobuf-encoded request for getting current block height
func (q Connector) BlockNumber(req *librustgo.CosmosRequest_BlockNumber) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query BlockNumber invoked")
	blockHeight := big.NewInt(q.Ctx.BlockHeight())
	return proto.Marshal(&librustgo.QueryBlockNumberResponse{Number: blockHeight.Bytes()})
}

// ChainId handles incoming protobuf-encoded request for getting network chain id
func (q Connector) ChainId(req *librustgo.CosmosRequest_ChainId) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query ChainId invoked")
	chainId := q.Keeper.ChainID()
	return proto.Marshal(&librustgo.QueryChainIdResponse{ChainId: chainId.Bytes()})
}

// InsertStorageCell handles incoming protobuf-encoded request for updating state of storage cell
func (q Connector) InsertStorageCell(req *librustgo.CosmosRequest_InsertStorageCell) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query InsertStorageCell invoked")
	q.Keeper.SetState(
		q.Ctx,
		common.BytesToAddress(req.InsertStorageCell.Address),
		common.BytesToHash(req.InsertStorageCell.Index),
		req.InsertStorageCell.Value,
	)
	return proto.Marshal(&librustgo.QueryInsertStorageCellResponse{})
}

// GetStorageCell handles incoming protobuf-encoded request of storage cell value
func (q Connector) GetStorageCell(req *librustgo.CosmosRequest_StorageCell) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query Request value of storage cell")
	value := q.Keeper.GetState(
		q.Ctx,
		common.BytesToAddress(req.StorageCell.Address),
		common.BytesToHash(req.StorageCell.Index),
	)
	return proto.Marshal(&librustgo.QueryGetAccountStorageCellResponse{Value: value.Bytes()})
}

// IsAccountExists handles incoming protobuf-encoded request to check if account exists
func (q Connector) IsAccountExists(req *librustgo.CosmosRequest_ContainsKey) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query Request to check if account exists")
	accountPtr := q.Keeper.GetAccount(q.Ctx, common.BytesToAddress(req.ContainsKey.Key))
	return proto.Marshal(&librustgo.QueryContainsKeyResponse{Contains: accountPtr != nil})
}

// GetAccountCode handles incoming protobuf-encoded request and returns bytecode associated
// with given account. If account does not exist, it returns empty response
func (q Connector) GetAccountCode(req *librustgo.CosmosRequest_AccountCode) ([]byte, error) {
	q.Ctx.Logger().Debug("Connector::Query Request account code")
	account := q.Keeper.GetAccountWithoutBalance(q.Ctx, common.BytesToAddress(req.AccountCode.Address))
	if account == nil {
		return proto.Marshal(&librustgo.QueryGetAccountCodeResponse{})
	}
	return proto.Marshal(&librustgo.QueryGetAccountCodeResponse{
		Code: q.Keeper.GetCode(q.Ctx, common.BytesToHash(account.CodeHash)),
	})
}
