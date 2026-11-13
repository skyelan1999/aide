package server

// Built-in recipe text; materialized plans remain editable user content.
var businessRecipeEnglish = map[string]string{
	"软件维护":      "Software maintenance",
	"理解问题":      "Understand the problem",
	"问题范围与预期行为": "Problem scope and expected behavior",
	"已记录实际问题、预期行为和影响范围": "Actual problem, expected behavior and scope recorded",
	"缺少输入已明确列出":         "Missing inputs explicitly listed",
	"复现与影响分析":           "Reproduce and assess impact",
	"复现记录与影响清单":         "Reproduction record and impact list",
	"复现步骤和实际结果可核对":      "Reproduction steps and actual results are inspectable",
	"相关代码、文档和测试已复核":     "Related code, documents and tests reviewed",
	"实现修改":              "Implement changes",
	"可审查的代码与文件差异":       "Reviewable code and file changes",
	"修改对应已确认的问题范围":      "Changes address the confirmed problem scope",
	"文件已应用且未覆盖无关工作":     "Files applied without overwriting unrelated work",
	"回归验证":              "Run regressions",
	"实际测试结果与剩余限制":       "Actual test results and remaining limits",
	"已运行相关回归并核对结果":      "Relevant regressions run and results checked",
	"失败和未执行项已单独记录":      "Failures and unexecuted checks recorded separately",
	"审查与交付":             "Review and deliver",
	"交付说明与审查材料":         "Delivery notes and review materials",
	"交付物、差异和测试证据可追溯":    "Deliverables, changes and test evidence are traceable",
	"外部PR或发布状态按实际回执记录":  "External PR or publication state follows actual receipts",
	"工程交付":              "Engineering delivery",
	"核对工程资料":            "Review engineering inputs",
	"资料来源、版本与缺口清单":      "Input sources, versions and gaps",
	"输入来源和适用版本已经核对":     "Input sources and applicable versions checked",
	"缺少参数和约束已明确记录":      "Missing parameters and constraints explicitly recorded",
	"计算与分析":             "Calculate and analyze",
	"计算依据、假设与结果":        "Calculation basis, assumptions and results",
	"计算输入、单位与依据可追溯":     "Calculation inputs, units and basis are traceable",
	"未确认假设和适用边界已列出":     "Unconfirmed assumptions and applicability limits listed",
	"生成报告或图纸":           "Produce reports or drawings",
	"可编辑的报告或图纸文件":       "Editable report or drawing files",
	"内容对应已确认输入和计算结果":    "Contents match confirmed inputs and calculations",
	"交付文件已实际保存":         "Deliverable files actually saved",
	"校核成果":              "Check deliverables",
	"一致性与完整性校核记录":       "Consistency and completeness review",
	"资料、计算和成果内容一致性已检查":  "Consistency between inputs, calculations and outputs checked",
	"缺陷和待核实项已明确记录":      "Defects and open verification items explicitly recorded",
	"修订与交付":             "Revise and deliver",
	"修订成果与交付清单":         "Revised outputs and delivery list",
	"校核缺口已处理或明确阻塞":      "Review gaps resolved or explicitly blocked",
	"实际文件、版本和交付状态可核对":   "Actual files, versions and delivery state are inspectable",
}

func businessRecipesLanguage(language string) []flowRecipe {
	recipes := businessRecipes()
	if language == "en" {
		for i := range recipes {
			r := &recipes[i]
			r.Name = businessRecipeEnglish[r.Name]
			for j := range r.Steps {
				s := &r.Steps[j]
				s.Title = businessRecipeEnglish[s.Title]
				s.Deliverable = businessRecipeEnglish[s.Deliverable]
				for n, text := range s.Checks {
					s.Checks[n] = businessRecipeEnglish[text]
				}
			}
		}
	}
	return recipes
}
