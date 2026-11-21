package server

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestShellMemoryBudgetValidation(t *testing.T) {
	for _, v := range []any{nil, float64(128), float64(768), float64(1536)} {
		ctx, err := shellMemoryBudgetContext(context.Background(), v)
		if err != nil || shellMemoryBudget(ctx) < 128 || shellMemoryBudget(ctx) > 1536 {
			t.Fatalf("valid memory policy: %v %v", v, err)
		}
	}
	for _, v := range []any{"128", float64(127), float64(1537), 128.5, math.NaN(), math.Inf(1)} {
		if _, err := shellMemoryBudgetContext(context.Background(), v); err == nil {
			t.Fatalf("invalid memory budget accepted: %v", v)
		}
	}
	if shellMemoryBudget(context.Background()) != 768 {
		t.Fatal("missing context must retain default")
	}
}

func TestShellMemoryFailureReturnsRepairGuidance(t *testing.T) {
	text := analyzeShellFailure("python3 process-source.dwg.py", "", -1, errors.New("command memory budget exceeded: 800 MiB RSS > 768 MiB"))
	if !strings.Contains(text, "超过作业内存预算") || !strings.Contains(text, "更小批次") || strings.Contains(text, "不要再探测/安装") {
		t.Fatalf("resource failure misclassified: %s", text)
	}
}
