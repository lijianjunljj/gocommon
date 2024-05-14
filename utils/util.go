package utils

import "fmt"

// 全排列
func Permute[T any](nums []T) [][]T {
	var results [][]T
	PermuteRecursion(nums, 0, len(nums)-1, &results)
	fmt.Println("results:", results)
	return results
}

func PermuteRecursion[T any](nums []T, l int, r int, results *[][]T) {
	if l == r {
		// 输出一个可能的全排列
		*results = append(*results, nums)
	} else {
		for i := l; i <= r; i++ {
			// 交换元素，进行排列
			nums[l], nums[i] = nums[i], nums[l]
			// 递归进行下一个元素的全排列
			PermuteRecursion(nums, l+1, r, results)
			// 回溯，恢复原状
			nums[l], nums[i] = nums[i], nums[l]
		}
	}
}
