// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

// TransportMessenger represents the behavior of a transport message
// that can be received and replied to.
type TransportMessenger interface {
	Subject() string
	Data() []byte
	Respond(data []byte) error
}
