package decimal

import "github.com/shopspring/decimal"

func Add(v1, v2 float64, round int32) (float64, bool) {
	return decimal.NewFromFloat(v1).Add(decimal.NewFromFloat(v2)).Round(round).Float64()
}

func Mul(v1, v2 float64, round int32) (float64, bool) {
	return decimal.NewFromFloat(v1).Mul(decimal.NewFromFloat(v2)).Round(round).Float64()
}
