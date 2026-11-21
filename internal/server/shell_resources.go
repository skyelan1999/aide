package server

import (
	"context"
	"fmt"
	"math"
)

type shellMemoryBudgetKey struct{}

func shellMemoryBudgetContext(ctx context.Context, value any) (context.Context, error) {
	budget := 768
	if value != nil {
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < 128 || n > 1536 {
			return nil, fmt.Errorf("memoryMiB must be an integer from 128 to1536")
		}
		budget = int(n)
	}
	return context.WithValue(ctx, shellMemoryBudgetKey{}, budget), nil
}

func shellMemoryBudget(ctx context.Context) int {
	budget, ok := ctx.Value(shellMemoryBudgetKey{}).(int)
	if !ok || budget < 128 || budget > 1536 {
		return 768
	}
	return budget
}
