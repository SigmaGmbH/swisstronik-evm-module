package keeper

import (
	"errors"
	"github.com/SigmaGmbH/librustgo"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/golang/protobuf/proto"
	"math/big"
)

// Connector allows our VM interact with existing Cosmos application.
// It is passed by pointer into SGX to make it accessible for our VM.
type Connector struct {
	// StateDB used to store intermediate state
	StateDB *statedb.StateDB
	// GetHashFn returns the hash corresponding to n
	GetHashFn vm.GetHashFunc
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
	}

	return nil, errors.New("wrong query received")
}

// GetAccount handles incoming protobuf-encoded request for account data such as balance and nonce.
// Returns data in protobuf-encoded format
func (q Connector) GetAccount(req *librustgo.CosmosRequest_GetAccount) ([]byte, error) {
	//println("Connector::Query GetAccount invoked")

	ethAddress := common.BytesToAddress(req.GetAccount.Address)
	balance := q.StateDB.GetBalance(ethAddress)
	nonce := q.StateDB.GetNonce(ethAddress)

	return proto.Marshal(&librustgo.QueryGetAccountResponse{
		Balance: balance.Bytes(),
		Nonce:   sdk.Uint64ToBigEndian(nonce),
	})
}

// ContainsKey handles incoming protobuf-encoded request to check whether specified address exists
func (q Connector) ContainsKey(req *librustgo.CosmosRequest_ContainsKey) ([]byte, error) {
	//println("Connector::Query ContainsKey invoked")
	address := common.BytesToAddress(req.ContainsKey.Key)
	contains := q.StateDB.Exist(address)
	return proto.Marshal(&librustgo.QueryContainsKeyResponse{Contains: contains})
}

// InsertAccountCode handles incoming protobuf-encoded request for adding or modifying existing account code
// It will insert account code only if account exists, otherwise it returns an error
func (q Connector) InsertAccountCode(req *librustgo.CosmosRequest_InsertAccountCode) ([]byte, error) {
	//println("Connector::Query InsertAccountCode invoked")

	ethAddress := common.BytesToAddress(req.InsertAccountCode.Address)
	q.StateDB.SetCode(ethAddress, req.InsertAccountCode.Code)

	return proto.Marshal(&librustgo.QueryInsertAccountCodeResponse{})
}

// RemoveStorageCell handles incoming protobuf-encoded request for removing contract storage cell for given key (index)
func (q Connector) RemoveStorageCell(req *librustgo.CosmosRequest_RemoveStorageCell) ([]byte, error) {
	//println("Connector::Query RemoveStorageCell invoked")
	address := common.BytesToAddress(req.RemoveStorageCell.Address)
	index := common.BytesToHash(req.RemoveStorageCell.Index)

	q.StateDB.SetState(address, index, common.Hash{})

	return proto.Marshal(&librustgo.QueryRemoveStorageCellResponse{})
}

// Remove handles incoming protobuf-encoded request for removing smart contract (selfdestruct)
func (q Connector) Remove(req *librustgo.CosmosRequest_Remove) ([]byte, error) {
	//println("Connector::Query Remove invoked")

	ethAddress := common.BytesToAddress(req.Remove.Address)
	q.StateDB.Suicide(ethAddress)

	return proto.Marshal(&librustgo.QueryRemoveResponse{})
}

// BlockHash handles incoming protobuf-encoded request for getting block hash
func (q Connector) BlockHash(req *librustgo.CosmosRequest_BlockHash) ([]byte, error) {
	//println("Connector::Query BlockHash invoked")

	blockNumber := &big.Int{}
	blockNumber.SetBytes(req.BlockHash.Number)
	blockHash := q.GetHashFn(blockNumber.Uint64())

	return proto.Marshal(&librustgo.QueryBlockHashResponse{Hash: blockHash.Bytes()})
}

// InsertStorageCell handles incoming protobuf-encoded request for updating state of storage cell
func (q Connector) InsertStorageCell(req *librustgo.CosmosRequest_InsertStorageCell) ([]byte, error) {
	//println("Connector::Query InsertStorageCell invoked")

	ethAddress := common.BytesToAddress(req.InsertStorageCell.Address)
	index := common.BytesToHash(req.InsertStorageCell.Index)
	value := common.BytesToHash(req.InsertStorageCell.Value)

	q.StateDB.SetState(ethAddress, index, value)

	return proto.Marshal(&librustgo.QueryInsertStorageCellResponse{})
}

// GetStorageCell handles incoming protobuf-encoded request of storage cell value
func (q Connector) GetStorageCell(req *librustgo.CosmosRequest_StorageCell) ([]byte, error) {
	//println("Connector::Query Request value of storage cell")

	ethAddress := common.BytesToAddress(req.StorageCell.Address)
	index := common.BytesToHash(req.StorageCell.Index)
	value := q.StateDB.GetState(ethAddress, index)

	return proto.Marshal(&librustgo.QueryGetAccountStorageCellResponse{Value: value.Bytes()})
}

// GetAccountCode handles incoming protobuf-encoded request and returns bytecode associated
// with given account. If account does not exist, it returns empty response
func (q Connector) GetAccountCode(req *librustgo.CosmosRequest_AccountCode) ([]byte, error) {
	//println("Connector::Query Request account code")
	ethAddress := common.BytesToAddress(req.AccountCode.Address)
	code := q.StateDB.GetCode(ethAddress)

	return proto.Marshal(&librustgo.QueryGetAccountCodeResponse{
		Code: code,
	})
}

// InsertAccount handles incoming protobuf-encoded request for inserting new account data
// such as balance and nonce. If there is deployed contract behind given address, its bytecode
// or code hash won't be changed
func (q Connector) InsertAccount(req *librustgo.CosmosRequest_InsertAccount) ([]byte, error) {
	//println("Connector::Query Request to insert account code")
	ethAddress := common.BytesToAddress(req.InsertAccount.Address)

	balance := &big.Int{}
	balance.SetBytes(req.InsertAccount.Balance)

	nonce := &big.Int{}
	nonce.SetBytes(req.InsertAccount.Nonce)

	q.StateDB.SetBalance(ethAddress, balance)
	q.StateDB.SetNonce(ethAddress, nonce.Uint64())

	return proto.Marshal(&librustgo.QueryInsertAccountResponse{})
}
