package cmd

import (
	"dvdlevanon/kubernetes-database-scaler/pkg/cleaner"
	"dvdlevanon/kubernetes-database-scaler/pkg/controller"
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/op/go-logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var logger = logging.MustGetLogger("main")
var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "kubernetes-database-scaler",
	Short: "Dynamically duplicate Kubernetes deployments according to data in the DB",
	Long: `A Kubernetes controller that watch for a table in a Database

It creates a deployment per row in the DB, it useful for creating a pod per customer.
Conditions can be add to the query in order to filter some rows.

The original deployment used as a template when duplicating.
--target-deployment-name is a name of a column in the DB, the value of this column appended to the new deployment name

A list of environment variables can be passed to the new deployment, their values are the values from the DB	
`,
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
	usernameFile := viper.GetString("database-username-file")
	passwordFile := viper.GetString("database-password-file")
	tableName := viper.GetString("table-name")
	sqlCondition := viper.GetString("sql-condition")
	rawSql := viper.GetString("raw-sql")
	watcher, err := tablewatch.New(driver, host, port, dbname,
		username, password, usernameFile, passwordFile, tableName, sqlCondition, rawSql)
	if err != nil {
		return err
	}

	checkInterval := viper.GetInt("check-interval")
	go watcher.Watch(checkInterval, rows)
	return nil
}

func splitEnvironmentVariable(arr []string) []string {
	if len(arr) == 1 && strings.Contains(arr[0], ",") {
		// When using environment variable instead of command line args,
		//	we support passing multiple values separated by a comma
		//
		return strings.Split(arr[0], ",")
	}

	return arr
}

func setupVpaController(manager manager.Manager) (*controller.VpaReconciler, error) {
	originalVpaName := viper.GetString("original-vpa-name")

	if originalVpaName == "" {
		return nil, nil
	}

	if err := vpa_types.SchemeBuilder.AddToScheme(manager.GetScheme()); err != nil {
		return nil, err
	}

	originalVpaNamespace := viper.GetString("original-deployment-namespace")
	originalDeploymentName := viper.GetString("original-deployment-name")
	targetDeploymentName := viper.GetString("target-deployment-name")

	controller, err := controller.NewVpaController(manager.GetClient(),
		originalVpaNamespace, originalVpaName, targetDeploymentName, originalDeploymentName)
	if err != nil {
		return nil, err
	}

	if err := controller.SetupWithManager(manager); err != nil {
		return nil, err
	}

	return controller, nil
}

func setupDeploymentController(manager manager.Manager, removeDeploys <-chan string) (*controller.DeploymentReconciler, error) {
	originalDeploymentNamespace := viper.GetString("original-deployment-namespace")
	originalDeploymentName := viper.GetString("original-deployment-name")
	targetDeploymentName := viper.GetString("target-deployment-name")
	environments := splitEnvironmentVariable(viper.GetStringSlice("environment"))
	excludeLabels := splitEnvironmentVariable(viper.GetStringSlice("exclude-label"))
	controller, err := controller.New(manager.GetClient(),
		originalDeploymentNamespace, originalDeploymentName, targetDeploymentName, environments, excludeLabels, removeDeploys)
	if err != nil {
		return nil, err
	}

	if err := controller.SetupWithManager(manager); err != nil {
		return nil, err
	}

	return controller, nil
}

func watch() error {
	rows := make(chan tablewatch.Row)
	if err := setupWatcher(rows); err != nil {
		return err
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}

	manager, err := ctrl.NewManager(config, manager.Options{})
	if err != nil {
		return err
	}

	removeDeploys := make(chan string)
	checkInterval := viper.GetInt("check-interval")
	targetDeploymentName := viper.GetString("target-deployment-name")
	cleanInterval := time.Duration(checkInterval) * 3 * time.Second
	cleaner := cleaner.NewCleaner(cleanInterval, targetDeploymentName, removeDeploys)
	go cleaner.Run()

	deploymentController, err := setupDeploymentController(manager, removeDeploys)
	if err != nil {
		return err
	}

	vpaController, err := setupVpaController(manager)
	if err != nil {
		return err
	}

	go manager.Start(ctrl.SetupSignalHandler())
	go deploymentController.Run(cleaner)

	for row := range rows {
		deploymentController.OnRow(row)
		cleaner.OnRow(row)

		if vpaController != nil {
			vpaController.OnRow(row)
		}
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
	rootCmd.Flags().StringP("database-username-file", "", "", "A file containing a database username")
	rootCmd.Flags().StringP("database-password-file", "", "", "A file containing a database password")

	rootCmd.Flags().IntP("check-interval", "", 10, "Periodic check interval in seconds")
	rootCmd.Flags().StringP("table-name", "t", "", "Specify the database table to monitor for changes")
	rootCmd.Flags().StringP("sql-condition", "", "", "Filter rows using a WHERE clause (e.g., 'status = \"active\"')")
	rootCmd.Flags().StringP("raw-sql", "", "", "Execute a custom SQL query instead of using table-name and sql-condition (Warning: No SQL injection protection)")

	rootCmd.Flags().StringP("original-deployment-namespace", "", "", "Deployment namespace to duplicate")
	rootCmd.Flags().StringP("original-deployment-name", "", "", "Deployment name to duplicate")
	rootCmd.Flags().StringP("target-deployment-name", "", "", "A column name to append to the copied deployment")
	rootCmd.Flags().StringArrayP("environment", "", make([]string, 0), "Names of columns to add as environment variables")
	rootCmd.Flags().StringP("original-vpa-name", "", "", "A vertical pod autoscaler to duplicate")
	rootCmd.Flags().StringArrayP("exclude-label", "", make([]string, 0), "Specify label names to exclude from the duplicated deployment")

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
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
