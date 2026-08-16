package csharp

import "psl/internal/executor"

// ExecutionPlan runs .cs file-based apps with dotnet and .csx scripts with
// dotnet-script.
func ExecutionPlan(ext, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	if ext == "csx" {
		path, _, err := executor.Find("C# script", lookPath, "dotnet-script")
		if err != nil {
			return executor.Plan{}, err
		}
		return executor.OneStep(path, append([]string{source, "--"}, programArgs...)), nil
	}

	path, _, err := executor.Find("C#", lookPath, "dotnet")
	if err != nil {
		return executor.Plan{}, err
	}
	return executor.OneStep(path, append([]string{"run", "--file", source, "--"}, programArgs...)), nil
}
