package keeper_test

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/evmos/ethermint/x/evm/types"
	"math/big"
)

func (suite *KeeperTestSuite) TestNativeCurrencyTransfer() {
	var (
		err             error
		msg             *types.MsgEthereumTx
		signer          ethtypes.Signer
		vmdb            *statedb.StateDB
		chainCfg        *params.ChainConfig
		expectedGasUsed uint64
		transferAmount  int64
	)

	testCases := []struct {
		name     string
		malleate func()
		expErr   bool
	}{
		{
			"Transfer funds tx",
			func() {
				transferAmount = 1000
				msg, _, err = newEthMsgTx(
					vmdb.GetNonce(suite.address),
					suite.ctx.BlockHeight(),
					suite.address,
					chainCfg,
					suite.signer,
					signer,
					ethtypes.AccessListTxType,
					nil,
					nil,
					big.NewInt(transferAmount),
				)
				suite.Require().NoError(err)
				expectedGasUsed = params.TxGas
			},
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()

			keeperParams := suite.app.EvmKeeper.GetParams(suite.ctx)
			chainCfg = keeperParams.ChainConfig.EthereumConfig(suite.app.EvmKeeper.ChainID())
			signer = ethtypes.LatestSignerForChainID(suite.app.EvmKeeper.ChainID())
			vmdb = suite.StateDB()

			tc.malleate()

			err := suite.app.EvmKeeper.SetBalance(suite.ctx, suite.address, big.NewInt(100000))
			suite.Require().NoError(err)

			balanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
			receiverBalanceBefore := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})

			res, err := suite.app.EvmKeeper.HandleTx(suite.ctx, msg)
			if tc.expErr {
				suite.Require().Error(err)
				return
			}

			balanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, suite.address)
			suite.Require().Equal(
				balanceBefore.Sub(balanceBefore, big.NewInt(transferAmount)),
				balanceAfter,
			)

			receiverBalanceAfter := suite.app.EvmKeeper.GetBalance(suite.ctx, common.Address{})
			suite.Require().Equal(
				receiverBalanceBefore.Add(receiverBalanceBefore, big.NewInt(transferAmount)),
				receiverBalanceAfter,
			)

			suite.Require().NoError(err)
			suite.Require().Equal(expectedGasUsed, res.GasUsed)
			suite.Require().False(res.Failed())
		})
	}
}
