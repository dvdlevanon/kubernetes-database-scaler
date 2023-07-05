package tablewatch

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/op/go-logging"
)

type Row map[string]string

var logger = logging.MustGetLogger("tablewatch")

type Tablewatch struct {
	sqlQuery  string
	sqlParams []any
	dbConn    *dbConn
}

func getSqlCondition(conditions []string) (string, []any, error) {
	whereParts := make([]string, 0)
	queryParams := make([]any, 0)
	for i, condition := range conditions {
		parts := strings.Split(condition, "=")
		if len(parts) != 2 {
			return "", nil, fmt.Errorf("invalid condition %s", condition)
		}

		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", parts[0], i+1))
		queryParams = append(queryParams, parts[1])
	}

	whereClause := strings.Join(whereParts, " AND ")
	return whereClause, queryParams, nil
}

func getSqlQuery(tableName string, conditions []string) (string, []any, error) {
	if tableName == "" {
		return "", nil, fmt.Errorf("table name is missing")
	}

	where, params, err := getSqlCondition(conditions)
	if err != nil {
		return "", nil, err
	}

	if len(params) == 0 {
		return fmt.Sprintf("SELECT * FROM %s", tableName), nil, nil
	} else {
		return fmt.Sprintf("SELECT * FROM %s WHERE %s", tableName, where), params, nil
	}
}

func New(driver string, host string, port string, dbname string,
	username string, password string, usernameFile string, passwordFile string,
	tableName string, conditions []string) (*Tablewatch, error) {

	sqlQuery, sqlParams, err := getSqlQuery(tableName, conditions)
	if err != nil {
		return nil, err
	}

	dbConn := &dbConn{
		driver:       driver,
		host:         host,
		port:         port,
		dbname:       dbname,
		username:     username,
		password:     password,
		usernameFile: usernameFile,
		passwordFile: passwordFile,
	}

	err = dbConn.openAndVerify()
	if err != nil {
		return nil, err
	}

	go dbConn.watchDbCredentials()

	watcher := &Tablewatch{
		dbConn:    dbConn,
		sqlQuery:  sqlQuery,
		sqlParams: sqlParams,
	}

	return watcher, nil
}

func (w *Tablewatch) Watch(checkInterval int, output chan<- Row) {
	logger.Infof("SQL Query %s, params: %+q", w.sqlQuery, w.sqlParams)

	for {
		if err := w.periodicCheck(output); err != nil {
			logger.Errorf("Periodic check failed with %s", err)
		}

		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

func (w *Tablewatch) periodicCheck(output chan<- Row) error {
	logger.Debugf("Periodic check DB table")

	rows, err := w.dbConn.conn.Query(w.sqlQuery, w.sqlParams...)
	if err != nil {
		return err
	}

	defer rows.Close()
	return w.handleRows(rows, output)
}

func (w *Tablewatch) handleRows(rows *sql.Rows, output chan<- Row) error {
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	values := make([][]byte, len(columns))
	valuesPtr := make([]any, len(columns))
	for i := range values {
		valuesPtr[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuesPtr...); err != nil {
			logger.Errorf("Error reading row %s", err)
			continue
		}

		row := w.getRow(columns, valuesPtr)
		output <- row
	}

	return nil
}

func (w *Tablewatch) getRow(columns []string, values []any) Row {
	row := make(map[string]string, len(values))
	for i, valuePtr := range values {
		value := fmt.Sprintf("%s", valuePtr)

		// Remove the pointer sign "&" from the formatted string
		if len(value) > 1 {
			value = value[1:]
		}

		row[columns[i]] = value
	}

	return row
}
