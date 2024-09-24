package decimal

import "github.com/shopspring/decimal"

func Add(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(0)
	for _, v := range args {
		d = d.Add(decimal.NewFromFloat(v))
	}
	v, ok := d.Round(round).Float64()
	if ok {
		return v
	}
	return 0
}

func Mul(round int32, args ...float64) float64 {
	d := decimal.NewFromFloat(1)
	for _, v := range args {
		d = d.Mul(decimal.NewFromFloat(v))
	}
	v, ok := d.Round(round).Float64()
	if ok {
		return v
	}
	return 0
}
