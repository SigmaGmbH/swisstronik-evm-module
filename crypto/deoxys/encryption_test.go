package deoxys

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestKeyDerivation(t *testing.T) {
	rustStateKeyHex := "19c3288df186addcbf1a9fbab4e4be48aaa7d8468a955eea3326f5a63807142a"
	salt := []byte("test")
	masterKey := make([]byte, 32)

	stateKey := DeriveEncryptionKey(masterKey, salt)
	stateKeyHex := hex.EncodeToString(stateKey)

	if rustStateKeyHex != stateKeyHex {
		t.Fail()
	}
}

func TestStateEncryption(t *testing.T) {
	masterKey := make([]byte, 32)
	contractAddress := make([]byte, 20)
	storageValue := make([]byte, 32)

	encryptedState, err := EncryptState(masterKey, contractAddress, storageValue)
	if err != nil {
		t.Fatal(err)
	}

	decryptedValue, err := DecryptState(masterKey, contractAddress, encryptedState)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(storageValue, decryptedValue) {
		t.Fatal("original and decrypted values are not the same")
	}
}

func TestECDHEncryption(t *testing.T) {
	var userPrivateKey [32]byte
	rand.Read(userPrivateKey[:])

	var nodePrivateKey [32]byte
	rand.Read(nodePrivateKey[:])

	userPublicKey := GetCurve25519PublicKey(userPrivateKey)
	nodePublicKey := GetCurve25519PublicKey(nodePrivateKey)

	data := make([]byte, 32)

	encryptedData, err := EncryptECDH(userPrivateKey[:], nodePublicKey[:], data)
	if err != nil {
		t.Fatal(err)
	}

	decryptedDataOnNode, err := DecryptECDH(nodePrivateKey[:], userPublicKey[:], encryptedData)
	if err != nil {
		t.Fatal(err)
	}

	decryptedDataOnUser, err := DecryptECDH(userPrivateKey[:], nodePublicKey[:], encryptedData)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(decryptedDataOnNode, decryptedDataOnUser) {
		t.Fatal("decryption on node != user")
	}

	if !bytes.Equal(data, decryptedDataOnNode) {
		t.Fatal("decryption failed")
	}
}
