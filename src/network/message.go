// Package network defines wire metadata and errors for the real transport.
// It does not implement a virtual network or a simulated event queue.
package network

import (
	"blockchain-lab01/src/types"
	"errors"
)

type Kind string

const (
	Header  Kind = "header"
	Request Kind = "body_request"
	Body    Kind = "body"
	Vote    Kind = "vote"
)

var ErrQueueFull = errors.New("outbound queue full")

// Rejection marks an invalid peer message; internal failures must stop the node.
type Rejection struct{ Cause error }

func (e *Rejection) Error() string { return e.Cause.Error() }
func (e *Rejection) Unwrap() error { return e.Cause }
func Reject(err error) error {
	if err == nil {
		return nil
	}
	return &Rejection{err}
}

type Message struct {
	Kind          Kind
	Ref           types.Hash
	Height, Round uint64
	Data          []byte
}
type Packet struct {
	Message
	ID       uint64
	From, To int
}
type Event struct {
	At, Seq       uint64
	Node, Peer    int
	Event         string
	Type          Kind
	Height, Round uint64
	Ref           types.Hash
	Packet        uint64
	Reason        string
	Phase, Step   uint8
	StateRoot     string `json:",omitempty"`
	Validator     string `json:",omitempty"`
}
