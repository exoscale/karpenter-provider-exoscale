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

func IsInstanceNotFoundError(err error) bool {
	var instanceNotFoundErr *InstanceNotFoundError
	return errors.As(err, &instanceNotFoundErr)
}

type InsufficientCapacityError struct {
	Err error
}

func (e *InsufficientCapacityError) Error() string {
	return fmt.Sprintf("insufficient capacity: %v", e.Err)
}

func (e *InsufficientCapacityError) Unwrap() error {
	return e.Err
}

func NewInsufficientCapacityError(err error) *InsufficientCapacityError {
	return &InsufficientCapacityError{
		Err: err,
	}
}

func IsInsufficientCapacityError(err error) bool {
	var capacityErr *InsufficientCapacityError
	return errors.As(err, &capacityErr)
}
