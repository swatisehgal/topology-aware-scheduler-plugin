package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	plugin "github.com/swatisehgal/topology-aware-scheduler-plugin/pkg/plugin"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "topology-aware-scheduler-plugin",
	Short: "Topology Aware Scheduler plugin",
	Long:  `Topology Aware Scheduler plugin.`,
	Run: func(cmd *cobra.Command, args []string) {
		run(cmd.Args)
	},
	FParseErrWhitelist: cobra.FParseErrWhitelist{
		UnknownFlags: true,
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.AutomaticEnv() // read in environment variables that match
}

func run(args cobra.PositionalArgs) {
	command := app.NewSchedulerCommand(
		app.WithPlugin(plugin.Name, plugin.NewTopologyAwareScheduler),
	)
	command.Args = args
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
