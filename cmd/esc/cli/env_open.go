// Copyright 2023, Pulumi Corporation.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/pulumi/esc"
	"github.com/pulumi/esc/cmd/esc/cli/client"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
)

func newEnvOpenCmd(envcmd *envCommand) *cobra.Command {
	var duration time.Duration
	var format string

	cmd := &cobra.Command{
		Use:   "open [<org-name>/]<environment-name> [property path]",
		Args:  cobra.MaximumNArgs(2),
		Short: "Open the environment with the given name.",
		Long: "Open the environment with the given name and return the result\n" +
			"\n" +
			"This command opens the environment with the given name. The result is written to\n" +
			"stdout as JSON. If a property path is specified, only retrieves that property.\n",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if err := envcmd.esc.getCachedClient(ctx); err != nil {
				return err
			}

			orgName, envName, args, err := envcmd.getEnvName(args)
			if err != nil {
				return err
			}
			_ = args

			var path resource.PropertyPath
			if len(args) == 1 {
				p, err := resource.ParsePropertyPath(args[0])
				if err != nil {
					return fmt.Errorf("invalid property path %v: %w", args[0], err)
				}
				path = p
			}

			switch format {
			case "detailed", "json", "string":
				// OK
			case "dotenv", "shell":
				if len(path) != 0 {
					return fmt.Errorf("output format '%s' may not be used with a property path", format)
				}
			default:
				return fmt.Errorf("unknown output format %q", format)
			}

			env, diags, err := envcmd.openEnvironment(ctx, orgName, envName, duration)
			if err != nil {
				return err
			}
			if len(diags) != 0 {
				return envcmd.writePropertyEnvironmentDiagnostics(envcmd.esc.stderr, diags)
			}

			return renderValue(envcmd.esc.stdout, env, path, format)
		},
	}

	cmd.Flags().DurationVarP(
		&duration, "lifetime", "l", 2*time.Hour,
		"the lifetime of the opened environment in the form HhMm (e.g. 2h, 1h30m, 15m)")
	cmd.Flags().StringVarP(
		&format, "format", "f", "json",
		"the output format to use. May be 'dotenv', 'json', 'detailed', or 'shell'")

	return cmd
}

func renderValue(
	out io.Writer,
	env *esc.Environment,
	path resource.PropertyPath,
	format string,
) error {
	if env == nil {
		return nil
	}

	val := esc.NewValue(env.Properties)
	if len(path) != 0 {
		if vv, ok := getEnvValue(val, path); ok {
			val = *vv
		} else {
			val = esc.Value{}
		}
	}

	switch format {
	case "json":
		body := val.ToJSON(false)
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(body)
	case "detailed":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(val)
	case "dotenv":
		for _, kvp := range getEnvironmentVariables(env) {
			fmt.Fprintln(out, kvp)
		}
		return nil
	case "shell":
		for _, kvp := range getEnvironmentVariables(env) {
			fmt.Fprintf(out, "export %v\n", kvp)
		}
		return nil
	case "string":
		fmt.Fprintf(out, "%v\n", val.ToString(false))
		return nil
	default:
		// NOTE: we shouldn't get here. This was checked at the beginning of the function.
		return fmt.Errorf("unknown output format %q", format)
	}

}

func getEnvironmentVariables(env *esc.Environment) []string {
	vars, ok := env.Properties["environmentVariables"].Value.(map[string]esc.Value)
	if !ok {
		return nil
	}
	keys := maps.Keys(vars)
	sort.Strings(keys)

	var environ []string
	for _, k := range keys {
		v := vars[k]
		if strValue, ok := v.Value.(string); ok {
			environ = append(environ, fmt.Sprintf("%v=%q", k, strValue))
		}
	}
	return environ
}

func (env *envCommand) openEnvironment(
	ctx context.Context,
	orgName string,
	envName string,
	duration time.Duration,
) (*esc.Environment, []client.EnvironmentDiagnostic, error) {
	envID, diags, err := env.esc.client.OpenEnvironment(ctx, orgName, envName, duration)
	if err != nil {
		return nil, nil, err
	}
	if len(diags) != 0 {
		return nil, diags, err
	}
	open, err := env.esc.client.GetOpenEnvironment(ctx, orgName, envName, envID)
	return open, nil, err
}