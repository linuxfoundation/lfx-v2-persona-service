// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package errors

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestNewValidation_messageOnly(t *testing.T) {
	err := NewValidation("bad input")
	assert.Equal(t, "bad input", err.Error())
}

func TestNewValidation_composesMessage(t *testing.T) {
	inner := stderrors.New("inner cause")
	err := NewValidation("bad input", inner)
	assert.Equal(t, "bad input: inner cause", err.Error())
}

func TestNewValidation_implementsError(t *testing.T) {
	var e error = NewValidation("bad input")
	require.NotNil(t, e)
}

func TestNewValidation_multipleWrappedErrors(t *testing.T) {
	a := stderrors.New("cause A")
	b := stderrors.New("cause B")
	err := NewValidation("composite", a, b)
	// errors.Join produces "cause A\ncause B"
	assert.Contains(t, err.Error(), "cause A")
	assert.Contains(t, err.Error(), "cause B")
}

// ---------------------------------------------------------------------------
// NotFound
// ---------------------------------------------------------------------------

func TestNewNotFound_messageOnly(t *testing.T) {
	err := NewNotFound("resource not found")
	assert.Equal(t, "resource not found", err.Error())
}

func TestNewNotFound_composesMessage(t *testing.T) {
	inner := stderrors.New("db miss")
	err := NewNotFound("not found", inner)
	assert.Equal(t, "not found: db miss", err.Error())
}

func TestNewNotFound_implementsError(t *testing.T) {
	var e error = NewNotFound("not found")
	require.NotNil(t, e)
}

// ---------------------------------------------------------------------------
// Unexpected
// ---------------------------------------------------------------------------

func TestNewUnexpected_messageOnly(t *testing.T) {
	err := NewUnexpected("something went wrong")
	assert.Equal(t, "something went wrong", err.Error())
}

func TestNewUnexpected_composesMessage(t *testing.T) {
	inner := stderrors.New("panic value")
	err := NewUnexpected("unexpected", inner)
	assert.Equal(t, "unexpected: panic value", err.Error())
}

func TestNewUnexpected_implementsError(t *testing.T) {
	var e error = NewUnexpected("unexpected")
	require.NotNil(t, e)
}

// ---------------------------------------------------------------------------
// ServiceUnavailable
// ---------------------------------------------------------------------------

func TestNewServiceUnavailable_messageOnly(t *testing.T) {
	err := NewServiceUnavailable("service down")
	assert.Equal(t, "service down", err.Error())
}

func TestNewServiceUnavailable_composesMessage(t *testing.T) {
	inner := stderrors.New("connection refused")
	err := NewServiceUnavailable("service down", inner)
	assert.Equal(t, "service down: connection refused", err.Error())
}

func TestNewServiceUnavailable_implementsError(t *testing.T) {
	var e error = NewServiceUnavailable("service down")
	require.NotNil(t, e)
}

// ---------------------------------------------------------------------------
// Type identity — each constructor returns the correct type
// ---------------------------------------------------------------------------

func TestErrorTypes_areDistinct(t *testing.T) {
	var v error = NewValidation("v")
	var n error = NewNotFound("n")
	var u error = NewUnexpected("u")
	var s error = NewServiceUnavailable("s")

	_, isValidation := v.(Validation)
	_, isNotFound := n.(NotFound)
	_, isUnexpected := u.(Unexpected)
	_, isSvcUnavail := s.(ServiceUnavailable)

	assert.True(t, isValidation)
	assert.True(t, isNotFound)
	assert.True(t, isUnexpected)
	assert.True(t, isSvcUnavail)

	// Cross-checks: types must not overlap
	_, notNF := v.(NotFound)
	_, notU := v.(Unexpected)
	assert.False(t, notNF)
	assert.False(t, notU)
}
