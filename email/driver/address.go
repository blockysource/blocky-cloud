package driver

import (
	"context"

	"gocloud.dev/gcerrors"
)

// AddressValidator is an interface that represents an implementation of the email address validator.
type AddressValidator interface {
	// ValidateAddressBatch lets the provider validate the input addresses in a batch.
	ValidateAddressBatch(ctx context.Context, batch []*Address) ([]*AddressValidationResult, error)

	// Close closes the provider and releases all resources.
	// This could be a noop for some providers.
	// Once closed is called, there will be no method calls to the AddressValidator other than As, ErrorAs, and ErrorCode.
	Close() error

	// ErrorAs allows providers to expose provider-specific types for returned errors.
	ErrorAs(err error, i interface{}) bool

	// ErrorCode should return a code that describes the error, which was returned by
	ErrorCode(error) gcerrors.ErrorCode

	// As allows providers to expose provider-specific types.
	As(i interface{}) bool
}

// Address represents an email address.
type Address struct {
	// Name is an optional name of the address i.e. "John Doe".
	Name string
	// Address is the proper email address.
	Address string
	// IsFormatValid defines if the format of the address is valid.
	// This field is automatically set to true as a result of ParseAddress or ParseAddressList.
	IsFormatValid bool
}

type AddressValidationResult struct {
}
