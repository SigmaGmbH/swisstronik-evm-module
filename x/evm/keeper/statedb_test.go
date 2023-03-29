package keeper_test

import (
	"fmt"
	"math/big"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/SigmaGmbH/evm-module/crypto/ethsecp256k1"
	"github.com/SigmaGmbH/evm-module/tests"
	"github.com/SigmaGmbH/evm-module/x/evm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func (suite *KeeperTestSuite) TestGetNonce() {
	testCases := []struct {
		name          string
		address       common.Address
		expectedNonce uint64
		malleate      func()
	}{
		{
			"account not found",
			tests.GenerateAddress(),
			0,
			func() {},
		},
		{
			"existing account",
			suite.address,
			1,
			func() {
				suite.Require().NoError(
					suite.app.EvmKeeper.SetNonce(suite.ctx, suite.address, 1),
				)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.malleate()

			nonce := suite.app.EvmKeeper.GetNonce(suite.ctx, tc.address)
			suite.Require().Equal(tc.expectedNonce, nonce)
		})
	}
}

func (suite *KeeperTestSuite) TestSetNonce() {
	testCases := []struct {
		name     string
		address  common.Address
		nonce    uint64
		malleate func()
	}{
		{
			"new account",
			tests.GenerateAddress(),
			10,
			func() {},
		},
		{
			"existing account",
			suite.address,
			99,
			func() {},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.Require().NoError(
				suite.app.EvmKeeper.SetNonce(suite.ctx, tc.address, tc.nonce),
			)
			nonce := suite.app.EvmKeeper.GetNonce(suite.ctx, tc.address)
			suite.Require().Equal(tc.nonce, nonce)
		})
	}
}

func (suite *KeeperTestSuite) TestGetCodeHash() {
	addr := tests.GenerateAddress()
	baseAcc := &authtypes.BaseAccount{Address: sdk.AccAddress(addr.Bytes()).String()}
	suite.app.AccountKeeper.SetAccount(suite.ctx, baseAcc)

	testCases := []struct {
		name     string
		address  common.Address
		expHash  common.Hash
		malleate func()
	}{
		{
			"account not found",
			tests.GenerateAddress(),
			common.BytesToHash(types.EmptyCodeHash),
			func() {},
		},
		{
			"account not EthAccount type, EmptyCodeHash",
			addr,
			common.BytesToHash(types.EmptyCodeHash),
			func() {},
		},
		{
			"existing account",
			suite.address,
			crypto.Keccak256Hash([]byte("codeHash")),
			func() {
				err := suite.app.EvmKeeper.SetAccountCode(suite.ctx, suite.address, []byte("codeHash"))
				suite.Require().NoError(err)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.malleate()

			hash := suite.app.EvmKeeper.GetAccountOrEmpty(suite.ctx, tc.address).CodeHash
			suite.Require().Equal(tc.expHash, common.BytesToHash(hash))
		})
	}
}

func (suite *KeeperTestSuite) TestSetCode() {
	addr := tests.GenerateAddress()
	baseAcc := &authtypes.BaseAccount{Address: sdk.AccAddress(addr.Bytes()).String()}
	suite.app.AccountKeeper.SetAccount(suite.ctx, baseAcc)

	testCases := []struct {
		name    string
		address common.Address
		code    []byte
		isNoOp  bool
	}{
		{
			"account not found",
			tests.GenerateAddress(),
			[]byte("code"),
			false,
		},
		{
			"account not EthAccount type",
			addr,
			nil,
			true,
		},
		{
			"existing account",
			suite.address,
			[]byte("code"),
			false,
		},
		{
			"existing account, code deleted from store",
			suite.address,
			nil,
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			prev, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, tc.address)
			suite.Require().NoError(err)

			err = suite.app.EvmKeeper.SetAccountCode(suite.ctx, tc.address, tc.code)
			suite.Require().NoError(err)

			post, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, tc.address)
			suite.Require().NoError(err)

			if tc.isNoOp {
				suite.Require().Equal(prev, post)
			} else {
				suite.Require().Equal(tc.code, post)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestKeeperSetAccountCode() {
	addr := tests.GenerateAddress()
	baseAcc := &authtypes.BaseAccount{Address: sdk.AccAddress(addr.Bytes()).String()}
	suite.app.AccountKeeper.SetAccount(suite.ctx, baseAcc)

	testCases := []struct {
		name string
		code []byte
	}{
		{
			"set code",
			[]byte("codecodecode"),
		},
		{
			"delete code",
			nil,
		},
	}

	suite.SetupTest()

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := suite.app.EvmKeeper.SetAccountCode(suite.ctx, addr, tc.code)
			suite.Require().NoError(err)

			acct := suite.app.EvmKeeper.GetAccountWithoutBalance(suite.ctx, addr)
			suite.Require().NotNil(acct)

			if tc.code != nil {
				suite.Require().True(acct.IsContract())
			}

			codeHash := crypto.Keccak256Hash(tc.code)
			suite.Require().Equal(codeHash.Bytes(), acct.CodeHash)

			code := suite.app.EvmKeeper.GetCode(suite.ctx, common.BytesToHash(acct.CodeHash))
			suite.Require().Equal(tc.code, code)
		})
	}
}

func (suite *KeeperTestSuite) TestKeeperSetCode() {
	addr := tests.GenerateAddress()
	baseAcc := &authtypes.BaseAccount{Address: sdk.AccAddress(addr.Bytes()).String()}
	suite.app.AccountKeeper.SetAccount(suite.ctx, baseAcc)

	testCases := []struct {
		name     string
		codeHash []byte
		code     []byte
	}{
		{
			"set code",
			[]byte("codeHash"),
			[]byte("this is the code"),
		},
		{
			"delete code",
			[]byte("codeHash"),
			nil,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.app.EvmKeeper.SetCode(suite.ctx, tc.codeHash, tc.code)
			key := suite.app.GetKey(types.StoreKey)
			store := prefix.NewStore(suite.ctx.KVStore(key), types.KeyPrefixCode)
			code := store.Get(tc.codeHash)

			suite.Require().Equal(tc.code, code)
		})
	}
}

func (suite *KeeperTestSuite) TestState() {
	testCases := []struct {
		name       string
		key, value common.Hash
	}{
		{
			"set state - delete from store",
			common.BytesToHash([]byte("key")),
			common.Hash{},
		},
		{
			"set state - update value",
			common.BytesToHash([]byte("key")),
			common.BytesToHash([]byte("value")),
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.app.EvmKeeper.SetState(suite.ctx, suite.address, tc.key, tc.value.Bytes())
			value := suite.app.EvmKeeper.GetState(suite.ctx, suite.address, tc.key)
			suite.Require().Equal(tc.value, common.BytesToHash(value))
		})
	}
}

func (suite *KeeperTestSuite) TestSuicide() {
	code := []byte("code")
	err := suite.app.EvmKeeper.SetAccountCode(suite.ctx, suite.address, code)
	suite.Require().NoError(err)

	addedCode, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, suite.address)
	suite.Require().NoError(err)

	suite.Require().Equal(code, addedCode)
	// Add state to account
	for i := 0; i < 5; i++ {
		suite.app.EvmKeeper.SetState(suite.ctx, suite.address, common.BytesToHash([]byte(fmt.Sprintf("key%d", i))), []byte(fmt.Sprintf("value%d", i)))
	}

	// Generate 2nd address
	privkey, _ := ethsecp256k1.GenerateKey()
	key, err := privkey.ToECDSA()
	suite.Require().NoError(err)
	addr2 := crypto.PubkeyToAddress(key.PublicKey)

	// Add code and state to account 2
	err = suite.app.EvmKeeper.SetAccountCode(suite.ctx, addr2, code)
	suite.Require().NoError(err)

	addedCode2, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, addr2)
	suite.Require().Equal(code, addedCode2)
	for i := 0; i < 5; i++ {
		suite.app.EvmKeeper.SetState(suite.ctx, addr2, common.BytesToHash([]byte(fmt.Sprintf("key%d", i))), []byte(fmt.Sprintf("value%d", i)))
	}

	// Destroy first contract
	err = suite.app.EvmKeeper.DeleteAccount(suite.ctx, suite.address)
	suite.Require().NoError(err)

	// Check code is deleted
	accCode, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, suite.address)
	suite.Require().NoError(err)
	suite.Require().Nil(accCode)

	// Check state is deleted
	var storage types.Storage
	suite.app.EvmKeeper.ForEachStorage(suite.ctx, suite.address, func(key, value common.Hash) bool {
		storage = append(storage, types.NewState(key, value))
		return true
	})
	suite.Require().Equal(0, len(storage))

	// Check account is deleted
	acc := suite.app.EvmKeeper.GetAccountOrEmpty(suite.ctx, suite.address)
	suite.Require().Equal(types.EmptyCodeHash, acc.CodeHash)

	// Check code is still present in addr2 and suicided is false
	code2, err := suite.app.EvmKeeper.GetAccountCode(suite.ctx, addr2)
	suite.Require().NoError(err)
	suite.Require().NotNil(code2)
}

func (suite *KeeperTestSuite) TestExist() {
	testCases := []struct {
		name     string
		address  common.Address
		malleate func()
		exists   bool
	}{
		{"success, account exists", suite.address, func() {}, true},
		{"success, account doesn't exist", tests.GenerateAddress(), func() {}, false},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.malleate()
			exists := suite.app.EvmKeeper.GetAccount(suite.ctx, tc.address) != nil
			suite.Require().Equal(tc.exists, exists)
		})
	}
}

func (suite *KeeperTestSuite) TestEmpty() {
	testCases := []struct {
		name     string
		address  common.Address
		malleate func()
		empty    bool
	}{
		{
			"not empty, positive balance",
			suite.address,
			func() {
				err := suite.app.EvmKeeper.SetBalance(suite.ctx, suite.address, big.NewInt(100))
				suite.Require().NoError(err)
			},
			false,
		},
		{"empty, account doesn't exist", tests.GenerateAddress(), func() {}, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			tc.malleate()
			isEmpty := suite.app.EvmKeeper.GetAccount(suite.ctx, tc.address) == nil
			suite.Require().Equal(tc.empty, isEmpty)
		})
	}
}

func (suite *KeeperTestSuite) CreateTestTx(msg *types.MsgHandleTx, priv cryptotypes.PrivKey) authsigning.Tx {
	option, err := codectypes.NewAnyWithValue(&types.ExtensionOptionsEthereumTx{})
	suite.Require().NoError(err)

	txBuilder := suite.clientCtx.TxConfig.NewTxBuilder()
	builder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
	suite.Require().True(ok)

	builder.SetExtensionOptions(option)

	err = msg.Sign(suite.ethSigner, tests.NewSigner(priv))
	suite.Require().NoError(err)

	err = txBuilder.SetMsgs(msg)
	suite.Require().NoError(err)

	return txBuilder.GetTx()
}

func (suite *KeeperTestSuite) TestForEachStorage() {
	var storage types.Storage

	testCase := []struct {
		name      string
		malleate  func()
		callback  func(key, value common.Hash) (stop bool)
		expValues []common.Hash
	}{
		{
			"aggregate state",
			func() {
				for i := 0; i < 5; i++ {
					suite.app.EvmKeeper.SetState(suite.ctx, suite.address, common.BytesToHash([]byte(fmt.Sprintf("key%d", i))), []byte(fmt.Sprintf("value%d", i)))
				}
			},
			func(key, value common.Hash) bool {
				storage = append(storage, types.NewState(key, value))
				return true
			},
			[]common.Hash{
				common.BytesToHash([]byte("value0")),
				common.BytesToHash([]byte("value1")),
				common.BytesToHash([]byte("value2")),
				common.BytesToHash([]byte("value3")),
				common.BytesToHash([]byte("value4")),
			},
		},
		{
			"filter state",
			func() {
				suite.app.EvmKeeper.SetState(suite.ctx, suite.address, common.BytesToHash([]byte("key")), []byte("value"))
				suite.app.EvmKeeper.SetState(suite.ctx, suite.address, common.BytesToHash([]byte("filterkey")), []byte("filtervalue"))
			},
			func(key, value common.Hash) bool {
				if value == common.BytesToHash([]byte("filtervalue")) {
					storage = append(storage, types.NewState(key, value))
					return false
				}
				return true
			},
			[]common.Hash{
				common.BytesToHash([]byte("filtervalue")),
			},
		},
	}

	for _, tc := range testCase {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			tc.malleate()

			suite.app.EvmKeeper.ForEachStorage(suite.ctx, suite.address, tc.callback)
			suite.Require().Equal(len(tc.expValues), len(storage), fmt.Sprintf("Expected values:\n%v\nStorage Values\n%v", tc.expValues, storage))

			vals := make([]common.Hash, len(storage))
			for i := range storage {
				vals[i] = common.HexToHash(storage[i].Value)
			}

			suite.Require().ElementsMatch(tc.expValues, vals)
		})
		storage = types.Storage{}
	}
}

func (suite *KeeperTestSuite) TestSetBalance() {
	amount := big.NewInt(-10)

	testCases := []struct {
		name     string
		addr     common.Address
		malleate func()
		expErr   bool
	}{
		{
			"address without funds - invalid amount",
			suite.address,
			func() {},
			true,
		},
		{
			"mint to address",
			suite.address,
			func() {
				amount = big.NewInt(100)
			},
			false,
		},
		{
			"burn from address",
			suite.address,
			func() {
				amount = big.NewInt(60)
			},
			false,
		},
		{
			"address with funds - invalid amount",
			suite.address,
			func() {
				amount = big.NewInt(-10)
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			tc.malleate()
			err := suite.app.EvmKeeper.SetBalance(suite.ctx, tc.addr, amount)
			if tc.expErr {
				suite.Require().Error(err)
			} else {
				balance := suite.app.EvmKeeper.GetBalance(suite.ctx, tc.addr)
				suite.Require().NoError(err)
				suite.Require().Equal(amount, balance)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestDeleteAccount() {
	supply := big.NewInt(100)
	contractAddr := suite.DeployTestContract(suite.T(), suite.address, supply)

	testCases := []struct {
		name   string
		addr   common.Address
		expErr bool
	}{
		{
			"remove address",
			suite.address,
			false,
		},
		{
			"remove unexistent address - returns nil error",
			common.HexToAddress("unexistent_address"),
			false,
		},
		{
			"remove deployed contract",
			contractAddr,
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			err := suite.app.EvmKeeper.DeleteAccount(suite.ctx, tc.addr)
			if tc.expErr {
				suite.Require().Error(err)
			} else {
				suite.Require().NoError(err)
				balance := suite.app.EvmKeeper.GetBalance(suite.ctx, tc.addr)
				suite.Require().Equal(new(big.Int), balance)
			}
		})
	}
}
