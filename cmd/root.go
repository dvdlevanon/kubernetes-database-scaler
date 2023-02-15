package cmd

import (
	"dvdlevanon/kubernetes-database-scaler/pkg/controller"
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"github.com/op/go-logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var logger = logging.MustGetLogger("main")
var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "kubernetes-database-scaler",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := watch()
		if err != nil {
			logger.Errorf("%s", err)
		}
	},
}

func setupWatcher(rows chan<- tablewatch.Row) error {
	driver := viper.GetString("database-driver")
	dbname := viper.GetString("database-name")
	port := viper.GetString("database-port")
	host := viper.GetString("database-host")
	username := viper.GetString("database-username")
	password := viper.GetString("database-password")
	tableName := viper.GetString("table-name")
	conditions := viper.GetStringSlice("condition")
	watcher, err := tablewatch.New(driver, host, port, dbname, username, password, tableName, conditions)
	if err != nil {
		return err
	}

	checkInterval := viper.GetInt("check-interval")
	go watcher.Watch(checkInterval, rows)
	return nil
}

func setupController() (*controller.DeploymentReconciler, error) {
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), manager.Options{})
	if err != nil {
		return nil, err
	}

	deploymentNamespace := viper.GetString("deployment-namespace")
	deploymentName := viper.GetString("deployment-name")
	deploymentColumnName := viper.GetString("deployment-column-name")
	controller, err := controller.New(manager.GetClient(), deploymentNamespace, deploymentName, deploymentColumnName)
	if err != nil {
		return nil, err
	}

	if err := controller.SetupWithManager(manager); err != nil {
		return nil, err
	}

	go manager.Start(ctrl.SetupSignalHandler())
	return controller, nil
}

func watch() error {
	rows := make(chan tablewatch.Row)
	if err := setupWatcher(rows); err != nil {
		return err
	}

	controller, err := setupController()
	if err != nil {
		return err
	}

	for row := range rows {
		controller.OnRow(row)
	}

	return nil
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func configureLogger() {
	logFormat := `[%{time:2006-01-02 15:04:05.000}] %{color}%{level:-7s}%{color:reset} %{message} [%{module} - %{shortfile}]`
	formatter, err := logging.NewStringFormatter(logFormat)
	if err != nil {
		// continue with default config
		return
	}

	logging.SetBackend(logging.NewLogBackend(os.Stdout, "", 0))
	logging.SetFormatter(formatter)
}

func init() {
	configureLogger()
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kubernetes-database-scaler.yaml)")

	rootCmd.Flags().StringP("database-driver", "", "", "Database driver name (postgres, mysql e.g.)")
	rootCmd.Flags().StringP("database-name", "", "", "Database name")
	rootCmd.Flags().StringP("database-port", "", "", "Database port")
	rootCmd.Flags().StringP("database-host", "", "", "Database hostname")
	rootCmd.Flags().StringP("database-username", "", "", "Database username")
	rootCmd.Flags().StringP("database-password", "", "", "Database password")

	rootCmd.Flags().StringP("table-name", "t", "", "Database table to watch")
	rootCmd.Flags().StringArrayP("condition", "", make([]string, 0), "Only rows match this condition will be fetched, can be specified multiple times - ('column-name=value')")

	rootCmd.Flags().IntP("check-interval", "", 10, "Periodic check interval in seconds")

	rootCmd.Flags().StringP("deployment-namespace", "", "", "Deployment namespace to duplicate")
	rootCmd.Flags().StringP("deployment-name", "", "", "Deployment name to duplicate")
	rootCmd.Flags().StringP("deployment-column-name", "", "", "A column name to append to the copied deployment")

	viper.BindPFlags(rootCmd.Flags())
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kubernetes-database-scaler")
	}

	viper.SetEnvPrefix("kubernetes_database_scaler")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
