// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package store

import "errors"

var (
	// ErrNotFound is returned when the requested entity does not exist.
	ErrNotFound = errors.New("store: entity not found")

	// ErrConflict is returned when an entity with the same identifier already exists.
	ErrConflict = errors.New("store: entity already exists")

	// ErrGateExpired is returned when attempting to resolve an expired gate.
	ErrGateExpired = errors.New("store: gate has expired")

	// ErrGateAlreadyResolved is returned when a gate has already been resolved.
	ErrGateAlreadyResolved = errors.New("store: gate has already been resolved")

	// ErrInvalidInput is returned when arguments are missing or malformed.
	ErrInvalidInput = errors.New("store: invalid input argument")

	// ErrStoreClosed is returned when operations are attempted on a closed store.
	ErrStoreClosed = errors.New("store: store is closed")
)
