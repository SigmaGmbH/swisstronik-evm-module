package keeper_test

import (
	"github.com/SigmaGmbH/librustgo"
	"github.com/ethereum/go-ethereum/common"
	evmkeeper "github.com/evmos/ethermint/x/evm/keeper"
	"github.com/golang/protobuf/proto"
	"math/big"
)

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
				// Create request
				addressToSet := common.BigToAddress(big.NewInt(100))
				balanceToSet := big.NewInt(10000)
				nonceToSet := big.NewInt(1)

				// Encode request
				request, encodeErr := proto.Marshal(&librustgo.CosmosRequest{
					Req: &librustgo.CosmosRequest_InsertAccount{
						InsertAccount: &librustgo.QueryInsertAccount{
							Address: addressToSet.Bytes(),
							Balance: balanceToSet.Bytes(),
							Nonce:   nonceToSet.Bytes(),
						},
					},
				})
				suite.Require().NoError(encodeErr)

				// Send request to the connector
				responseBytes, queryErr := connector.Query(request)
				suite.Require().NoError(queryErr)

				response := &librustgo.QueryInsertAccountResponse{}
				decodingError := proto.Unmarshal(responseBytes, response)
				suite.Require().NoError(decodingError)

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

			},
		},
		{
			"Should be able to get account data",
			func() {

			},
		},
		{
			"Should be able to set account code",
			func() {

			},
		},
		{
			"Should be able to get account code",
			func() {

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
