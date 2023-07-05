
# Kubernetes Database Scaler

Kubernetes Database Scaler is a custom Kubernetes controller. It's designed to automate the creation of Kubernetes deployments based on the rows in a specified database table. This tool is valuable for creating isolated environments (pods) per customer for systems with multi-tenant architecture. By dynamically generating deployments based on database rows, you can scale out your Kubernetes deployments in a very efficient and controlled manner.

Each row in the database table corresponds to a Kubernetes deployment. You can even add conditions to query specific rows, allowing for finer control over which rows translate into deployments. This is useful when only a subset of rows are needed to create deployments.

The deployments are not created from scratch; instead, an existing deployment is duplicated and customized for each row. The customization includes appending a value from a specific database column to the new deployment's name, which aids in the identification of the deployments. 

In addition, you can specify database columns whose values will be added as environment variables to the new deployments. This can be used to pass specific configuration or runtime data from your database to the Kubernetes deployments.

## Building

To build the Kubernetes Database Scaler, run the following command:

```bash
make build
```

## Running

To run the Kubernetes Database Scaler, execute:

```bash
./build/kubernetes-database-scaler
```

## Usage

Here is the usage information for Kubernetes Database Scaler:

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

## Building Docker Image

To build the Docker image for Kubernetes Database Scaler, use the following command:

```bash
make docker
```

## Running Docker Container

To run the Kubernetes Database Scaler as a Docker container, execute:

```bash
docker run \
	-e "KUBERNETES_DATABASE_SCALER_DATABASE_DRIVER=<db_driver>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_NAME=<db_name>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_PORT=<db_port>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_HOST=<db_hostname>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_USERNAME=<db_username>" \
  -e "KUBERNETES_DATABASE_SCALER_DATABASE_PASSWORD=<db_password>" \
  -e "KUBERNETES_DATABASE_SCALER_CHECK_INTERVAL=<check_interval_seconds>" \
  -e "KUBERNETES_DATABASE_SCALER_TABLE_NAME=<db_tablename>" \
  -e "KUBERNETES_DATABASE_SCALER_CONDITION=<column_name_1>=<value1>,<column_name_2>=<value2>" \
  -e "KUBERNETES_DATABASE_SCALER_ORIGINAL_DEPLOYMENT_NAMESPACE=<kubernetes_namespace>" \
  -e "KUBERNETES_DATABASE_SCALER_ORIGINAL_DEPLOYMENT_NAME=<kubernetes_deployment_name>" \
  -e "KUBERNETES_DATABASE_SCALER_TARGET_DEPLOYMENT_NAME=<column_name>" \
  -e "KUBERNETES_DATABASE_SCALER_ENVIRONMENT=<env1>=<column_name1>,<env2>=<column_name2>" \
	-it kubernetes-database-scaler:latest
```
