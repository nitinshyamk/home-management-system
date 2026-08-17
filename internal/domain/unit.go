package domain

import (
	"errors"
	"fmt"
)

// Dimension is the physical quantity a unit measures.
type Dimension string

const (
	DimensionCount  Dimension = "Count"
	DimensionMass   Dimension = "Mass"
	DimensionVolume Dimension = "Volume"
	DimensionLength Dimension = "Length"
)

// UnitCode identifies a unit in the reference table.
type UnitCode string

// Unit is reference data, not user-managed.
//
// ToBaseFactor is an integer because each dimension's base is its *smallest*
// unit: g, ml, cm, count. Choosing the largest instead would make cm→m a
// fraction, which fights the exact-integer design of Quantity.
type Unit struct {
	Code         UnitCode
	Dimension    Dimension
	ToBaseFactor int64
}

// ErrDimensionMismatch is U1: conversion across dimensions is undefined and must
// be rejected, never coerced. Consuming 100 ml from a mass-tracked holding is a
// question with no answer, not a rounding problem.
var ErrDimensionMismatch = errors.New("cannot convert between dimensions")

// ErrInexactConversion means the result cannot be represented exactly at Scale.
//
// Rejecting rather than truncating is deliberate: every quantity in this system
// is exact, and H10 compares replayed state to stored state for equality. A
// conversion that silently loses a thousandth would surface later as an
// integrity failure with no obvious cause.
var ErrInexactConversion = errors.New("conversion is not exact")

// Convert restates q, expressed in `from`, in terms of `to`.
func Convert(q Quantity, from, to Unit) (Quantity, error) {
	if from.Dimension != to.Dimension {
		return Zero, fmt.Errorf("%w: %s (%s) to %s (%s)",
			ErrDimensionMismatch, from.Code, from.Dimension, to.Code, to.Dimension)
	}
	if from.ToBaseFactor <= 0 || to.ToBaseFactor <= 0 {
		return Zero, fmt.Errorf("unit %s or %s has a non-positive base factor", from.Code, to.Code)
	}
	if from.Code == to.Code {
		return q, nil
	}

	inBase, err := q.Mul(from.ToBaseFactor)
	if err != nil {
		return Zero, err
	}
	if inBase.Milli()%to.ToBaseFactor != 0 {
		return Zero, fmt.Errorf("%w: %s %s in %s", ErrInexactConversion, q, from.Code, to.Code)
	}
	return FromMilli(inBase.Milli() / to.ToBaseFactor), nil
}
