package keeper

import (
	errorsmod "cosmossdk.io/errors"
	"errors"
	"github.com/SigmaGmbH/librustgo"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/golang/protobuf/proto"
	"math/big"
)

// Connector allows our VM interact with existing Cosmos application.
// It is passed by pointer into SGX to make it accessible for our VM.
type Connector struct {
	Ctx    sdk.Context
	Keeper *Keeper
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
		return q.InsertAccount(request)
	// Handles request if such account exists
	case *librustgo.CosmosRequest_ContainsKey:
		return q.ContainsKey(request)
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
	println("Connector::Query GetAccount invoked")
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

// ContainsKey handles incoming protobuf-encoded request to check whether specified address exists
func (q Connector) ContainsKey(req *librustgo.CosmosRequest_ContainsKey) ([]byte, error) {
	println("Connector::Query ContainsKey invoked")
	address := common.BytesToAddress(req.ContainsKey.Key)
	acc := q.Keeper.GetAccountWithoutBalance(q.Ctx, address)
	return proto.Marshal(&librustgo.QueryContainsKeyResponse{Contains: acc != nil})
}

// InsertAccountCode handles incoming protobuf-encoded request for adding or modifying existing account code
// It will insert account code only if account exists, otherwise it returns an error
func (q Connector) InsertAccountCode(req *librustgo.CosmosRequest_InsertAccountCode) ([]byte, error) {
	println("Connector::Query InsertAccountCode invoked")

	// Calculate code hash
	codeHash := crypto.Keccak256(req.InsertAccountCode.Code)
	q.Keeper.SetCode(q.Ctx, codeHash, req.InsertAccountCode.Code)

	// Link address with code hash
	cosmosAddr := sdk.AccAddress(req.InsertAccountCode.Address)
	cosmosAccount := q.Keeper.accountKeeper.GetAccount(q.Ctx, cosmosAddr)
	ethAccount := cosmosAccount.(ethermint.EthAccountI)

	// Set code hash if account exists
	if ethAccount != nil {
		if err := ethAccount.SetCodeHash(common.BytesToHash(codeHash)); err != nil { // TODO: Seems like it does not set code hash correctly
			return nil, err
		}
	} else {
		return nil, errors.New("cannot insert account code. Account does not exist")
	}

	return proto.Marshal(&librustgo.QueryInsertAccountCodeResponse{})
}

// RemoveStorageCell handles incoming protobuf-encoded request for removing contract storage cell for given key (index)
func (q Connector) RemoveStorageCell(req *librustgo.CosmosRequest_RemoveStorageCell) ([]byte, error) {
	println("Connector::Query RemoveStorageCell invoked")
	address := common.BytesToAddress(req.RemoveStorageCell.Address)
	index := common.BytesToHash(req.RemoveStorageCell.Index)

	q.Keeper.SetState(q.Ctx, address, index, nil)

	return proto.Marshal(&librustgo.QueryRemoveStorageCellResponse{})
}

// Remove handles incoming protobuf-encoded request for removing smart contract (selfdestruct)
func (q Connector) Remove(req *librustgo.CosmosRequest_Remove) ([]byte, error) {
	println("Connector::Query Remove invoked")
	address := common.BytesToAddress(req.Remove.Address)
	err := q.Keeper.DeleteAccount(q.Ctx, address)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove account")
	}
	return proto.Marshal(&librustgo.QueryRemoveResponse{})
}

// BlockHash handles incoming protobuf-encoded request for getting block hash
func (q Connector) BlockHash(req *librustgo.CosmosRequest_BlockHash) ([]byte, error) {
	println("Connector::Query BlockHash invoked")
	h := q.Ctx.HeaderHash()
	return proto.Marshal(&librustgo.QueryBlockHashResponse{Hash: h.Bytes()})
}

// BlockTimestamp handles incoming protobuf-encoded request for getting last block timestamp
func (q Connector) BlockTimestamp(req *librustgo.CosmosRequest_BlockTimestamp) ([]byte, error) {
	println("Connector::Query BlockTimestamp invoked")
	t := big.NewInt(q.Ctx.BlockTime().Unix())
	return proto.Marshal(&librustgo.QueryBlockTimestampResponse{Timestamp: t.Bytes()})
}

// BlockNumber handles incoming protobuf-encoded request for getting current block height
func (q Connector) BlockNumber(req *librustgo.CosmosRequest_BlockNumber) ([]byte, error) {
	println("Connector::Query BlockNumber invoked")
	blockHeight := big.NewInt(q.Ctx.BlockHeight())
	return proto.Marshal(&librustgo.QueryBlockNumberResponse{Number: blockHeight.Bytes()})
}

// ChainId handles incoming protobuf-encoded request for getting network chain id
func (q Connector) ChainId(req *librustgo.CosmosRequest_ChainId) ([]byte, error) {
	println("Connector::Query ChainId invoked")
	chainId := q.Keeper.ChainID()
	return proto.Marshal(&librustgo.QueryChainIdResponse{ChainId: chainId.Bytes()})
}

// InsertStorageCell handles incoming protobuf-encoded request for updating state of storage cell
func (q Connector) InsertStorageCell(req *librustgo.CosmosRequest_InsertStorageCell) ([]byte, error) {
	println("Connector::Query InsertStorageCell invoked")
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
	println("Connector::Query Request value of storage cell")
	value := q.Keeper.GetState(
		q.Ctx,
		common.BytesToAddress(req.StorageCell.Address),
		common.BytesToHash(req.StorageCell.Index),
	)

	return proto.Marshal(&librustgo.QueryGetAccountStorageCellResponse{Value: value.Bytes()})
}

// GetAccountCode handles incoming protobuf-encoded request and returns bytecode associated
// with given account. If account does not exist, it returns empty response
func (q Connector) GetAccountCode(req *librustgo.CosmosRequest_AccountCode) ([]byte, error) {
	println("Connector::Query Request account code")
	account := q.Keeper.GetAccountWithoutBalance(q.Ctx, common.BytesToAddress(req.AccountCode.Address))
	if account == nil {
		return proto.Marshal(&librustgo.QueryGetAccountCodeResponse{})
	}
	return proto.Marshal(&librustgo.QueryGetAccountCodeResponse{
		Code: q.Keeper.GetCode(q.Ctx, common.BytesToHash(account.CodeHash)),
	})
}

// InsertAccount handles incoming protobuf-encoded request for inserting new account data
// such as balance and nonce. If there is deployed contract behind given address, its bytecode
// or code hash won't be changed
func (q Connector) InsertAccount(req *librustgo.CosmosRequest_InsertAccount) ([]byte, error) {
	println("Connector::Query Request to insert account code")
	ethAddress := common.BytesToAddress(req.InsertAccount.Address)
	account := q.Keeper.GetAccountWithoutBalance(q.Ctx, ethAddress)

	balance := &big.Int{}
	balance.SetBytes(req.InsertAccount.Balance)

	nonce := &big.Int{}
	nonce.SetBytes(req.InsertAccount.Nonce)

	println("Insert balance: ", balance.String(), ", To address: ", ethAddress.String(), "\n")

	var accountData statedb.Account
	if account == nil {
		accountData = statedb.Account{
			Nonce:    nonce.Uint64(),
			Balance:  balance,
			CodeHash: nil,
		}
	} else {
		accountData = statedb.Account{
			Nonce:    nonce.Uint64(),
			Balance:  balance,
			CodeHash: account.CodeHash,
		}
	}

	if err := q.Keeper.SetAccount(q.Ctx, ethAddress, accountData); err != nil {
		return nil, errorsmod.Wrap(err, "Cannot set account")
	}

	return proto.Marshal(&librustgo.QueryInsertAccountResponse{})
}
