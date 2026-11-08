package crypto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"blockchain-lab01/src/types"
)

// GenerateIdentity uses operating-system cryptographic randomness, never a
// network fault seed. Test fixtures must not be used as real identities.
func GenerateIdentity() (types.PublicKey, ed25519.PrivateKey, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return types.PublicKey{}, nil, err
	}
	return types.PublicKey(pub), key, nil
}

func HashBytes(data []byte) types.Hash { return types.Hash(sha256.Sum256(data)) }

func TransactionID(tx types.Transaction) (types.Hash, error) {
	b, err := CanonicalSignedTxBytes(tx)
	if err != nil {
		return types.Hash{}, err
	}
	return HashBytes(b), nil
}
func BlockHash(h types.BlockHeader) (types.Hash, error) {
	b, err := CanonicalHeaderBytes(h)
	if err != nil {
		return types.Hash{}, err
	}
	return HashBytes(b), nil
}
func BodyHash(chainID string, body types.BlockBody) (types.Hash, error) {
	b, err := CanonicalBodyBytes(chainID, body)
	if err != nil {
		return types.Hash{}, err
	}
	return HashBytes(b), nil
}

func sign(key ed25519.PrivateKey, pub types.PublicKey, payload []byte) (types.Signature, error) {
	if len(key) != ed25519.PrivateKeySize {
		return types.Signature{}, fmt.Errorf("invalid private key length")
	}
	// Validate the cached public half too: arbitrary 64-byte inputs must not
	// produce signatures with an inconsistent identity.
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !bytes.Equal(derived, key) || !bytes.Equal(derived[32:], pub[:]) {
		return types.Signature{}, fmt.Errorf("private key does not match identity")
	}
	return types.Signature(ed25519.Sign(key, payload)), nil
}
func verify(expectedChain, chain string, pub types.PublicKey, payload []byte, sig types.Signature) error {
	if expectedChain == "" || expectedChain != chain {
		return fmt.Errorf("unexpected chain ID")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub[:]), payload, sig[:]) {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

// Sign functions return a copy; failed signing never partially changes input.
func SignTransaction(tx types.Transaction, key ed25519.PrivateKey) (types.Transaction, error) {
	b, err := CanonicalTxBytes(tx)
	if err != nil {
		return tx, err
	}
	sig, err := sign(key, tx.Sender, b)
	if err == nil {
		tx.Signature = sig
	}
	return tx, err
}
func VerifyTransaction(expectedChain string, tx types.Transaction) error {
	b, err := CanonicalTxBytes(tx)
	if err != nil {
		return err
	}
	return verify(expectedChain, tx.ChainID, tx.Sender, b, tx.Signature)
}
func SignHeader(h types.BlockHeader, key ed25519.PrivateKey) (types.BlockHeader, error) {
	b, err := CanonicalHeaderBytes(h)
	if err != nil {
		return h, err
	}
	sig, err := sign(key, types.PublicKey(h.Proposer), b)
	if err == nil {
		h.Signature = sig
	}
	return h, err
}
func VerifyHeader(expectedChain string, h types.BlockHeader) error {
	b, err := CanonicalHeaderBytes(h)
	if err != nil {
		return err
	}
	return verify(expectedChain, h.ChainID, types.PublicKey(h.Proposer), b, h.Signature)
}
func SignVote(v types.Vote, key ed25519.PrivateKey) (types.Vote, error) {
	b, err := CanonicalVoteBytes(v)
	if err != nil {
		return v, err
	}
	sig, err := sign(key, types.PublicKey(v.Validator), b)
	if err == nil {
		v.Signature = sig
	}
	return v, err
}
func VerifyVote(expectedChain string, v types.Vote) error {
	b, err := CanonicalVoteBytes(v)
	if err != nil {
		return err
	}
	return verify(expectedChain, v.ChainID, types.PublicKey(v.Validator), b, v.Signature)
}
