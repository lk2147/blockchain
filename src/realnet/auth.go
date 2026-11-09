package realnet

import (
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"unicode/utf8"

	pb "blockchain-lab01/src/proto"
	"blockchain-lab01/src/types"
)

// TransportBytes authenticates routing/control as well as payload. Version 1;
// uint32-length-prefixed TRANSPORT:<chain>, uint32 sender/recipient, uint64
// sequence, length-prefixed kind, fixed 32-byte reference, uint64 height/round,
// uint32-length-prefixed payload. All integers big-endian; signature excluded.
func TransportBytes(e *pb.Envelope) ([]byte, error) {
	if e == nil || e.ChainId == "" || !utf8.ValidString(e.ChainId) || len(e.Reference) != 32 {
		return nil, fmt.Errorf("invalid transport envelope")
	}
	switch e.Kind {
	case "header", "body_request", "body", "vote", "hello":
	default:
		return nil, fmt.Errorf("invalid transport kind")
	}
	b := []byte{1}
	blob := func(v []byte) error {
		if uint64(len(v)) > uint64(^uint32(0)) {
			return fmt.Errorf("transport field too large")
		}
		b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
		b = append(b, v...)
		return nil
	}
	if err := blob([]byte("TRANSPORT:" + e.ChainId)); err != nil {
		return nil, err
	}
	b = binary.BigEndian.AppendUint32(b, e.Sender)
	b = binary.BigEndian.AppendUint32(b, e.Recipient)
	b = binary.BigEndian.AppendUint64(b, e.Sequence)
	if err := blob([]byte(e.Kind)); err != nil {
		return nil, err
	}
	b = append(b, e.Reference...)
	b = binary.BigEndian.AppendUint64(b, e.Height)
	b = binary.BigEndian.AppendUint64(b, e.Round)
	if err := blob(e.Payload); err != nil {
		return nil, err
	}
	return b, nil
}
func VerifyEnvelope(chain string, recipient int, ids []types.ValidatorID, e *pb.Envelope) error {
	if e == nil || e.ChainId != chain || int(e.Sender) >= len(ids) {
		return fmt.Errorf("unexpected transport chain/identity")
	}
	if e.Recipient == ^uint32(0) {
		if e.Kind != "header" && e.Kind != "vote" {
			return fmt.Errorf("only headers/votes may be broadcast")
		}
	} else if e.Recipient != uint32(recipient) {
		return fmt.Errorf("unexpected transport recipient")
	}
	b, err := TransportBytes(e)
	if err != nil {
		return err
	}
	pub := ids[e.Sender]
	if !ed25519.Verify(ed25519.PublicKey(pub[:]), b, e.Signature) {
		return fmt.Errorf("invalid transport signature")
	}
	return nil
}
