/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/runner"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// NewCmdBuild describes the CLI command to build artifacts.
func NewCmdBuild(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Builds the artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return build(out, filename)
		},
	}
	AddRunDevFlags(cmd)
	return cmd
}

func build(out io.Writer, filename string) error {
	ctx := context.Background()

	config, err := readConfiguration(filename)
	if err != nil {
		return errors.Wrap(err, "reading configuration")
	}

	runner, err := runner.NewForConfig(opts, config)
	if err != nil {
		return errors.Wrap(err, "creating runner")
	}

	bRes, err := runner.Build(ctx, out, runner.Tagger, config.Build.Artifacts)
	if err != nil {
		return errors.Wrap(err, "build step")
	}

	for _, build := range bRes {
		fmt.Fprintln(out, build.ImageName, "->", build.Tag)
	}

	return err
}
