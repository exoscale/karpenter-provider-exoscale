package errors

import (
	"errors"
	"fmt"
)

type InstanceNotFoundError struct {
	InstanceID string
}

func (e *InstanceNotFoundError) Error() string {
	return fmt.Sprintf("instance %s not found", e.InstanceID)
}

func NewInstanceNotFoundError(instanceID string) *InstanceNotFoundError {
	return &InstanceNotFoundError{
		InstanceID: instanceID,
	}
}

type NetworkAttachmentError struct {
	InstanceID string
	NetworkID  string
	Err        error
}

func (e *NetworkAttachmentError) Error() string {
	return fmt.Sprintf("failed to attach network %s to instance %s: %v", e.NetworkID, e.InstanceID, e.Err)
}

func (e *NetworkAttachmentError) Unwrap() error {
	return e.Err
}

func NewNetworkAttachmentError(instanceID, networkID string, err error) *NetworkAttachmentError {
	return &NetworkAttachmentError{
		InstanceID: instanceID,
		NetworkID:  networkID,
		Err:        err,
	}
}

func IsInstanceNotFoundError(err error) bool {
	var instanceNotFoundErr *InstanceNotFoundError
	return errors.As(err, &instanceNotFoundErr)
}
