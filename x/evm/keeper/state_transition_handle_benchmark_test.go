package keeper_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/stretchr/testify/require"
)

func newHandleMsgTx(
	nonce uint64,
	blockHeight int64,
	address common.Address,
	cfg *params.ChainConfig,
	krSigner keyring.Signer,
	ethSigner ethtypes.Signer,
	txType byte,
	data []byte,
	accessList ethtypes.AccessList,
	value *big.Int,
) (*evmtypes.MsgHandleTx, *big.Int, error) {
	var (
		ethTx   *ethtypes.Transaction
		baseFee *big.Int
	)
	switch txType {
	case ethtypes.LegacyTxType:
		templateLegacyTx.Nonce = nonce
		if data != nil {
			templateLegacyTx.Data = data
		}
		if value != nil {
			templateLegacyTx.Value = value
		}
		ethTx = ethtypes.NewTx(templateLegacyTx)
	case ethtypes.AccessListTxType:
		templateAccessListTx.Nonce = nonce
		if data != nil {
			templateAccessListTx.Data = data
		} else {
			templateAccessListTx.Data = []byte{}
		}

		if value != nil {
			templateAccessListTx.Value = value
		}

		templateAccessListTx.AccessList = accessList
		ethTx = ethtypes.NewTx(templateAccessListTx)
	case ethtypes.DynamicFeeTxType:
		templateDynamicFeeTx.Nonce = nonce

		if data != nil {
			templateAccessListTx.Data = data
		} else {
			templateAccessListTx.Data = []byte{}
		}

		if value != nil {
			templateAccessListTx.Value = value
		}

		templateAccessListTx.AccessList = accessList
		ethTx = ethtypes.NewTx(templateDynamicFeeTx)
		baseFee = big.NewInt(3)
	default:
		return nil, baseFee, errors.New("unsupport tx type")
	}

	msg := &evmtypes.MsgHandleTx{}
	msg.FromEthereumTx(ethTx)
	msg.From = address.Hex()

	return msg, baseFee, msg.Sign(ethSigner, krSigner)
}

func BenchmarkHandleTx(b *testing.B) {
	suite := KeeperTestSuite{enableLondonHF: true}
	suite.SetupTestWithT(b)

	signer := ethtypes.LatestSignerForChainID(suite.app.EvmKeeper.ChainID())

	setBalanceError := suite.app.EvmKeeper.SetBalance(suite.ctx, suite.address, big.NewInt(10000000000000))
	require.NoError(b, setBalanceError)
	keeperParams := suite.app.EvmKeeper.GetParams(suite.ctx)
	chainCfg := keeperParams.ChainConfig.EthereumConfig(suite.app.EvmKeeper.ChainID())

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		msg, _, _ := newHandleMsgTx(
			suite.app.EvmKeeper.GetNonce(suite.ctx, suite.address),
			suite.ctx.BlockHeight(),
			suite.address,
			chainCfg,
			suite.signer,
			signer,
			ethtypes.AccessListTxType,
			nil,
			nil,
			big.NewInt(100),
		)

		b.StartTimer()
		resp, err := suite.app.EvmKeeper.HandleTx(suite.ctx, msg)
		b.StopTimer()
		require.NoError(b, err)
		require.Empty(b, resp.VmError)
	}
}
