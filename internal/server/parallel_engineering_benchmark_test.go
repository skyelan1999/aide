package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Synthetic engineering resource choices with an exhaustive, model-independent
// oracle. No timing sleeps, external services, user project data or tools.
type engineeringItem struct{ Cost, Capacity, Value int }
type engineeringCase struct {
	Items               []engineeringItem
	Budget, Capacity    int
	Requires, Conflicts [][2]int // zero-based item indices
}
type engineeringAnswer struct {
	Selected []int `json:"selected"` // one-based item identifiers
	Cost     int   `json:"cost"`
	Capacity int   `json:"capacity"`
	Value    int   `json:"value"`
}

func engineeringTotals(c engineeringCase, mask int) (cost, capacity, value int, feasible bool) {
	for _, pair := range c.Requires {
		if mask&(1<<pair[0]) != 0 && mask&(1<<pair[1]) == 0 {
			return 0, 0, 0, false
		}
	}
	for _, pair := range c.Conflicts {
		if mask&(1<<pair[0]) != 0 && mask&(1<<pair[1]) != 0 {
			return 0, 0, 0, false
		}
	}
	for i, item := range c.Items {
		if mask&(1<<i) != 0 {
			cost += item.Cost
			capacity += item.Capacity
			value += item.Value
		}
	}
	return cost, capacity, value, cost <= c.Budget && capacity <= c.Capacity
}
func engineeringOptimum(c engineeringCase) int {
	best := 0
	for mask := 0; mask < 1<<len(c.Items); mask++ {
		_, _, v, ok := engineeringTotals(c, mask)
		if ok && v > best {
			best = v
		}
	}
	return best
}
func engineeringWorkload(code, threshold string) (string, []string, []engineeringCase) {
	prompt := fmt.Sprintf("合成工程资源选择分析草案：下面十项相互独立，不执行工具。首段原样复述识别码 %s、阈值 %s，并注明尚未执行验证。每项都是0/1选择：每个候选最多选一次；成本和容量均不能超过上限；a→b表示选a必须选b；a×b表示两者不能同时选。目标是使价值总和最大，不可用近似值冒充精确最优。各项按原标签单独输出，一个JSON对象，格式为{\"selected\":[整数ID],\"cost\":总成本,\"capacity\":总容量,\"value\":总价值}，允许多个最优解中的任何一个。不要输出求解程序或外部执行声明。若不能确定最优，明确说明未求解，不编造数值。候选元组格式为ID:(成本,容量,价值)。\n", code, threshold)
	var labels []string
	var cases []engineeringCase
	for k := 0; k < 10; k++ {
		c := engineeringCase{Budget: 68 + k%3*3, Capacity: 51 + k%4*2, Requires: [][2]int{{3, 0}, {7, 4}, {13, 8}}, Conflicts: [][2]int{{1, 5}, {9, 12}, {15, 17}}}
		label := fmt.Sprintf("[CASE%02d]", k+1)
		line := fmt.Sprintf("%s 独立资源方案%02d，上限成本%d、容量%d，先决关系4→1,8→5,14→9，互斥2×6,10×13,16×18。候选：", label, k+1, c.Budget, c.Capacity)
		for i := 0; i < 18; i++ {
			item := engineeringItem{Cost: 3 + (i*7+k*5)%17, Capacity: 2 + (i*5+k*3)%13, Value: 11 + (i*13+k*17)%43}
			c.Items = append(c.Items, item)
			line += fmt.Sprintf("%d:(%d,%d,%d) ", i+1, item.Cost, item.Capacity, item.Value)
		}
		prompt += line + "\n"
		labels = append(labels, label)
		cases = append(cases, c)
	}
	return prompt, labels, cases
}
func validateEngineeringAnswer(text string, c engineeringCase, optimum int) error {
	if strings.Count(text, "{") != 1 || strings.Count(text, "}") != 1 {
		return fmt.Errorf("expected one complete JSON solution")
	}
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return fmt.Errorf("missing JSON solution")
	}
	var answer engineeringAnswer
	dec := json.NewDecoder(strings.NewReader(text[start:]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&answer); err != nil {
		return err
	}
	mask := 0
	for _, id := range answer.Selected {
		if id < 1 || id > len(c.Items) || mask&(1<<(id-1)) != 0 {
			return fmt.Errorf("invalid or duplicate item ID")
		}
		mask |= 1 << (id - 1)
	}
	cost, capacity, value, ok := engineeringTotals(c, mask)
	if !ok {
		return fmt.Errorf("infeasible selection")
	}
	if answer.Cost != cost || answer.Capacity != capacity || answer.Value != value {
		return fmt.Errorf("reported totals differ from selected items")
	}
	if value != optimum {
		return fmt.Errorf("suboptimal selection: got %d, optimum %d", value, optimum)
	}
	return nil
}
func TestEngineeringBenchmarkOracle(t *testing.T) {
	c := engineeringCase{Items: []engineeringItem{{2, 2, 5}, {3, 3, 7}, {1, 1, 6}}, Budget: 3, Capacity: 3}
	if engineeringOptimum(c) != 11 {
		t.Fatal("oracle disagrees with hand-calculated optimum")
	}
	if err := validateEngineeringAnswer(`{"selected":[1,3],"cost":3,"capacity":3,"value":11}`, c, 11); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"selected":[2],"cost":3,"capacity":3,"value":7}`,
		`{"selected":[1,3],"cost":3,"capacity":3,"value":11} {"selected":[]}`,
		`{"selected":[1,3],"cost":3,"capacity":3,"value":12}`,
		`{"selected":[1,1],"cost":4,"capacity":4,"value":10}`,
		`{"selected":[1,2],"cost":5,"capacity":5,"value":12}`,
	} {
		if validateEngineeringAnswer(bad, c, 11) == nil {
			t.Fatalf("invalid solution accepted: %s", bad)
		}
	}
	c.Requires = [][2]int{{2, 0}}
	if _, _, _, ok := engineeringTotals(c, 1<<2); ok {
		t.Fatal("missing prerequisite accepted")
	}
	c.Conflicts = [][2]int{{0, 2}}
	if _, _, _, ok := engineeringTotals(c, (1<<0)|(1<<2)); ok {
		t.Fatal("conflicting pair accepted")
	}
	prompt, labels, cases := engineeringWorkload("FIXTURE", "27.4 MHz")
	if len(explicitParallelTasks(prompt, 10)) != 10 || len(labels) != 10 || len(cases) != 10 {
		t.Fatal("engineering workload did not preserve ten independent assignments")
	}
}
