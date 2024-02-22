package tablewatch

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/xwb1989/sqlparser"
)

type Row map[string]string

var logger = logging.MustGetLogger("tablewatch")

type Tablewatch struct {
	sqlQuery string
	dbConn   *dbConn
}

// This function help to prevent sql injection using the where clause.
func isValidWhereClause(whereClause string) error {
	if whereClause == "" {
		return nil
	}

	stmt := fmt.Sprintf("select * from fake_table where %s", whereClause)
	_, err := sqlparser.Parse(stmt)
	if err != nil {
		// Parsing fail if the where clause contains more than a single sql statement, like this sql injection
		//
		// 	"'1' = '1'; TRUNCATE table;"
		//
		logger.Warningf("Invalid where clause %s", stmt)
		return err
	}

	disallowedPatterns := []string{
		";",        // Prevents multiple statements
		"--",       // Single line comment
		"xp_",      // Common prefix for SQL Server system stored procedures
		"/*", "*/", // Multi-line comment
		"truncate", "insert", "delete", "update", // DML operations
		"drop", "create", "alter", "grant", // DDL operations
		"shutdown", "exec", // Dangerous SQL Server commands
	}

	normalizedClause := strings.ToLower(whereClause)

	for _, pattern := range disallowedPatterns {
		if strings.Contains(normalizedClause, pattern) {
			return errors.New("disallowed pattern found in WHERE clause: " + pattern)
		}
	}

	return nil
}

func getSqlQuery(tableName string, sqlCondition string) (string, error) {
	if tableName == "" {
		return "", fmt.Errorf("table name is missing")
	}

	if err := isValidWhereClause(sqlCondition); err != nil {
		return "", err
	}

	where := sqlCondition

	if where != "" {
		return fmt.Sprintf("SELECT * FROM %s WHERE %s", tableName, where), nil
	} else {
		return fmt.Sprintf("SELECT * FROM %s", tableName), nil
	}
}

func New(driver string, host string, port string, dbname string,
	username string, password string, usernameFile string, passwordFile string,
	tableName string, sqlCondition string) (*Tablewatch, error) {

	sqlQuery, err := getSqlQuery(tableName, sqlCondition)
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
		dbConn:   dbConn,
		sqlQuery: sqlQuery,
	}

	return watcher, nil
}

func (w *Tablewatch) Watch(checkInterval int, output chan<- Row) {
	logger.Infof("SQL Query %s", w.sqlQuery)

	for {
		if err := w.periodicCheck(output); err != nil {
			logger.Errorf("Periodic check failed with %s", err)
		}

		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

func (w *Tablewatch) periodicCheck(output chan<- Row) error {
	logger.Debugf("Periodic check DB table")

	rows, err := w.dbConn.conn.Query(w.sqlQuery)
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
