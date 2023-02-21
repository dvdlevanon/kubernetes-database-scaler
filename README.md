
# Brief

A Kubernetes controller that watch for a table in a Database

It creates a deployment per row in the DB, it useful for creating a pod per customer.
Conditions can be add to the query in order to filter some rows.

The original deployment used as a template when duplicating.
--target-deployment-name is a name of a column in the DB, the value of this column appended to the new deployment name

A list of environment variables can be passed to the new deployment, their values are the values from the DB

# Build

`make build`

# Run

`./build/kubernetes-database-scaler --help`

```
Usage:
  kubernetes-database-scaler [flags]

Flags:
      --check-interval int                     Periodic check interval in seconds (default 10)
      --condition stringArray                  Only rows match this condition will be fetched, can be specified multiple times - ('column-name=value')
      --config string                          config file (default is $HOME/.kubernetes-database-scaler.yaml)
      --database-driver string                 Database driver name (postgres, mysql e.g.)
      --database-host string                   Database hostname
      --database-name string                   Database name
      --database-password string               Database password
      --database-port string                   Database port
      --database-username string               Database username
      --environment stringArray                Names of columns to add as environment variables
  -h, --help                                   help for kubernetes-database-scaler
      --original-deployment-name string        Deployment name to duplicate
      --original-deployment-namespace string   Deployment namespace to duplicate
  -t, --table-name string                      Database table to watch
      --target-deployment-name string          A column name to append to the copied deployment

```

# Docker

Build and run a docker image using those commands:
```
make docker

docker run \
	-e "KUBERNETES_DATABASE_SCALER_DATABASE_DRIVER=<db_driver>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_NAME=<db_name>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_PORT=<db_port>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_HOST=<db_hostname>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_USERNAME=<db_username>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_PASSWORD=<db_password>" \
  -e "KUBERNETES_DATABASE_SCALER_TABLE_NAME=<db_tablename>" \
  -e "KUBERNETES_DATABASE_SCALER_CONDITION=<column_name_1>=<value1>,<column_name_2>=<value2>" \
  -e "KUBERNETES_DATABASE_SCALER_ORIGINAL_DEPLOYMENT_NAMESPACE=<kubernetes_namespace>" \
  -e "KUBERNETES_DATABASE_SCALER_ORIGINAL_DEPLOYMENT_NAME=<kubernetes_deployment_name>" \
  -e "KUBERNETES_DATABASE_SCALER_TARGET_DEPLOYMENT_NAME=<column_name>" \
  -e "KUBERNETES_DATABASE_SCALER_ENVIRONMENT=<env1>=<column_name1>,<env2>=<column_name2>" \
	-it kubernetes-database-scaler:latest
```
