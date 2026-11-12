package show

import "github.com/glumpo/pileup/core/node"

type Interrupted struct {
	Pile   []byte
	Screen node.Screen
	Asset  []byte
}

func (h *Host) Interrupted() (Interrupted, error) {
	return Interrupted{}, ErrNoShow
}

func (h *Host) Forget() {}
