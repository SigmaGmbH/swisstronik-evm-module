package keeper_test

import (
	"errors"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/SigmaGmbH/evm-module/x/evm/keeper"
	"github.com/SigmaGmbH/evm-module/x/evm/types"
)

// FailureHook always fail
type FailureHook struct{}

func (dh FailureHook) PostTxProcessing(sdk.Context, core.Message, *ethtypes.Receipt) error {
	return errors.New("post tx processing failed")
}

func (suite *KeeperTestSuite) TestEvmHooks() {
	testCases := []struct {
		msg       string
		setupHook func() types.EvmHooks
		expFunc   func(hook types.EvmHooks, result error)
	}{
		{
			"always fail hook",
			func() types.EvmHooks {
				return &FailureHook{}
			},
			func(hook types.EvmHooks, result error) {
				suite.Require().Error(result)
			},
		},
	}

	for _, tc := range testCases {
		suite.SetupTest()
		hook := tc.setupHook()
		suite.app.EvmKeeper.SetHooks(keeper.NewMultiEvmHooks(hook))

		k := suite.app.EvmKeeper
		ctx := suite.ctx
		txHash := common.BigToHash(big.NewInt(1))

		receipt := &ethtypes.Receipt{
			TxHash: txHash,
			Logs:   nil,
		}
		result := k.PostTxProcessing(ctx, ethtypes.Message{}, receipt)

		tc.expFunc(hook, result)
	}
}
