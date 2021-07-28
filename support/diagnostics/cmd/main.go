package main

import (
	"log"
	"os"

	"github.com/elastic/cloud-on-k8s/support/diagnostics/internal"
	"github.com/spf13/cobra"
)

var (
	dumpParams = internal.DumpParams{}
)

func main() {
	cmd := &cobra.Command{
		Use:   "eck-diagnostics",
		Short: "ECK support diagnostics tool",
		Long:  "Dump ECK and Kubernetes data for support and troubleshooting purposes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return internal.RunDump(dumpParams)
		},
	}
	cmd.Flags().StringArrayVar(&dumpParams.OperatorNamespaces, "operator-namespaces", []string{"elastic-system"}, "Namespace(s) in which operator(s) are running")
	cmd.Flags().StringArrayVar(&dumpParams.ResourcesNamespaces, "resources-namespaces", []string{"default"}, "Namespace(s) in which resources are managed")
	cmd.Flags().StringVar(&dumpParams.OutputDir, "output-directory", "", "Path where to output dump files")
	cmd.Flags().BoolVar(&dumpParams.Verbose, "verbose", false, "Verbose mode")
	err := cmd.Execute()
	if err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
