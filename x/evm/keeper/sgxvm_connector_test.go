package keeper_test

import (
	"github.com/SigmaGmbH/librustgo"
	"github.com/ethereum/go-ethereum/common"
	evmkeeper "github.com/evmos/ethermint/x/evm/keeper"
	"github.com/golang/protobuf/proto"
	"math/big"
	"math/rand"
)

func insertAccount(
	connector *evmkeeper.Connector,
	address common.Address,
	balance, nonce *big.Int,
) error {
	// Encode request
	request, encodeErr := proto.Marshal(&librustgo.CosmosRequest{
		Req: &librustgo.CosmosRequest_InsertAccount{
			InsertAccount: &librustgo.QueryInsertAccount{
				Address: address.Bytes(),
				Balance: balance.Bytes(),
				Nonce:   nonce.Bytes(),
			},
		},
	})

	if encodeErr != nil {
		return encodeErr
	}

	responseBytes, queryErr := connector.Query(request)
	if queryErr != nil {
		return queryErr
	}

	response := &librustgo.QueryInsertAccountResponse{}
	decodingError := proto.Unmarshal(responseBytes, response)
	if decodingError != nil {
		return decodingError
	}

	return nil
}

func (suite *KeeperTestSuite) TestSGXVMConnector() {
	var (
		connector evmkeeper.Connector
	)

	connector = evmkeeper.Connector{
		Ctx:    suite.ctx,
		Keeper: suite.app.EvmKeeper,
	}

	testCases := []struct {
		name   string
		action func()
	}{
		{
			"Should be able to insert account",
			func() {
				addressToSet := common.BigToAddress(big.NewInt(rand.Int63n(100000)))
				balanceToSet := big.NewInt(10000)
				nonceToSet := big.NewInt(1)

				err := insertAccount(&connector, addressToSet, balanceToSet, nonceToSet)
				suite.Require().NoError(err)

				// Check if account was inserted correctly
				balance := connector.Keeper.GetBalance(connector.Ctx, addressToSet)
				nonce := connector.Keeper.GetNonce(connector.Ctx, addressToSet)

				suite.Require().Equal(balanceToSet, balance)
				suite.Require().Equal(nonceToSet.Uint64(), nonce)
			},
		},
		{
			"Should be able to check if account exists",
			func() {
				addressToSet := common.BigToAddress(big.NewInt(rand.Int63n(100000)))
				balanceToSet := big.NewInt(10000)
				nonceToSet := big.NewInt(1)

				err := insertAccount(&connector, addressToSet, balanceToSet, nonceToSet)
				suite.Require().NoError(err)

				// Encode request
				request, encodeErr := proto.Marshal(&librustgo.CosmosRequest{
					Req: &librustgo.CosmosRequest_ContainsKey{
						ContainsKey: &librustgo.QueryContainsKey{
							Key: addressToSet.Bytes(),
						},
					},
				})
				suite.Require().NoError(encodeErr)

				responseBytes, queryErr := connector.Query(request)
				suite.Require().NoError(queryErr)

				response := &librustgo.QueryContainsKeyResponse{}
				decodingError := proto.Unmarshal(responseBytes, response)
				suite.Require().NoError(decodingError)

				suite.Require().True(response.Contains)
			},
		},
		{
			"Should be able to get account data",
			func() {
				addressToSet := common.BigToAddress(big.NewInt(rand.Int63n(100000)))
				balanceToSet := big.NewInt(1400)
				nonceToSet := big.NewInt(22)

				err := insertAccount(&connector, addressToSet, balanceToSet, nonceToSet)
				suite.Require().NoError(err)

				// Encode request
				request, encodeErr := proto.Marshal(&librustgo.CosmosRequest{
					Req: &librustgo.CosmosRequest_GetAccount{
						GetAccount: &librustgo.QueryGetAccount{
							Address: addressToSet.Bytes(),
						},
					},
				})
				suite.Require().NoError(encodeErr)

				responseBytes, queryErr := connector.Query(request)
				suite.Require().NoError(queryErr)

				response := &librustgo.QueryGetAccountResponse{}
				decodingError := proto.Unmarshal(responseBytes, response)
				suite.Require().NoError(decodingError)

				returnedBalance := &big.Int{}
				returnedBalance.SetBytes(response.Balance)
				suite.Require().Equal(balanceToSet, returnedBalance)

				returnedNonce := &big.Int{}
				returnedNonce.SetBytes(response.Nonce)
				suite.Require().Equal(nonceToSet, returnedNonce)
			},
		},
		{
			"Should be able to set & get account code",
			func() {
				addressToSet := common.BigToAddress(big.NewInt(rand.Int63n(100000)))
				bytecode := make([]byte, 128)
				rand.Read(bytecode)

				suite.Require().NoError(
					insertAccount(&connector, addressToSet, big.NewInt(0), big.NewInt(0)),
				)

				//
				// Insert account code
				//
				request, encodeErr := proto.Marshal(&librustgo.CosmosRequest{
					Req: &librustgo.CosmosRequest_InsertAccountCode{
						InsertAccountCode: &librustgo.QueryInsertAccountCode{
							Address: addressToSet.Bytes(),
							Code:    bytecode,
						},
					},
				})
				suite.Require().NoError(encodeErr)

				responseBytes, queryErr := connector.Query(request)
				suite.Require().NoError(queryErr)

				response := &librustgo.QueryInsertAccountCodeResponse{}
				decodingError := proto.Unmarshal(responseBytes, response)
				suite.Require().NoError(decodingError)

				//
				// Request inserted account code
				//
				getRequest, getRequestErr := proto.Marshal(&librustgo.CosmosRequest{
					Req: &librustgo.CosmosRequest_AccountCode{
						AccountCode: &librustgo.QueryGetAccountCode{
							Address: addressToSet.Bytes(),
						},
					},
				})
				suite.Require().NoError(getRequestErr)

				responseAccountCodeBytes, queryAccountCodeErr := connector.Query(getRequest)
				suite.Require().NoError(queryAccountCodeErr)

				accountCodeResponse := &librustgo.QueryGetAccountCodeResponse{}
				accCodeDecodingErr := proto.Unmarshal(responseAccountCodeBytes, accountCodeResponse)
				suite.Require().NoError(accCodeDecodingErr)
				suite.Require().Equal(accountCodeResponse.Code, bytecode)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			tc.action()
		})
	}
}
