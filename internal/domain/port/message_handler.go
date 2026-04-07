// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package port

import "context"

// MessageHandler defines the behavior for handling persona requests.
type MessageHandler interface {
	GetPersona(ctx context.Context, msg TransportMessenger) ([]byte, error)
}
