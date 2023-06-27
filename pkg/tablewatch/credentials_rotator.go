package tablewatch

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type dbConn struct {
	driver       string
	host         string
	port         string
	dbname       string
	username     string
	password     string
	usernameFile string
	passwordFile string
	conn         *sql.DB
}

func (d *dbConn) getUsername() (string, error) {
	if d.username != "" {
		return d.username, nil
	}

	if d.usernameFile == "" {
		return "", nil
	}

	usernameBytes, err := os.ReadFile(d.usernameFile)
	if err != nil {
		return "", err
	}

	return strings.Trim(string(usernameBytes), " \n"), nil
}

func (d *dbConn) getPassword() (string, error) {
	if d.password != "" {
		return d.password, nil
	}

	if d.passwordFile == "" {
		return "", nil
	}

	passwordBytes, err := os.ReadFile(d.passwordFile)
	if err != nil {
		return "", err
	}

	return strings.Trim(string(passwordBytes), " \n"), nil
}

func (d *dbConn) buildPostgresConnectionInfo() (string, error) {
	password, err := d.getPassword()
	if err != nil {
		return "", err
	}

	username, err := d.getUsername()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s",
		d.host, d.port, username, password, d.dbname), nil
}

func (d *dbConn) openDbConnection() (*sql.DB, error) {
	var sqlconn *sql.DB
	var err error

	switch d.driver {
	case "postgres":
		var dsn string
		dsn, err = d.buildPostgresConnectionInfo()
		if err == nil {
			sqlconn, err = sql.Open("postgres", dsn)
		}
	default:
		err = fmt.Errorf("unsupported database driver %s", d.driver)
	}

	if err != nil {
		logger.Errorf("Error openning db connection %s", err)
		return nil, err
	}

	return sqlconn, err
}

func (d *dbConn) verifyDbConnection() error {
	if err := d.conn.Ping(); err != nil {
		logger.Errorf("Error pinging db %s", err)
		return err
	}

	return nil
}

func (d *dbConn) openAndVerify() error {
	conn, err := d.openDbConnection()
	if err != nil {
		return err
	}

	d.conn = conn
	return d.verifyDbConnection()
}

func (d *dbConn) watchDbCredentials() {
	if d.passwordFile == "" && d.usernameFile == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorf("Error initializing watcher %s", err)
		return
	}

	if d.passwordFile != "" {
		if err := watcher.Add(d.passwordFile); err != nil {
			logger.Errorf("Unable to watch password file %s %s", d.passwordFile, err)
			return
		}
	}

	if d.usernameFile != "" {
		if err := watcher.Add(d.usernameFile); err != nil {
			logger.Errorf("Unable to watch username file %s %s", d.usernameFile, err)
			return
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				logger.Infof("DB credentials file modified:", event.Name)
				if err := d.openAndVerify(); err != nil {
					logger.Errorf("Error openning db connection during rotation %s", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Infof("Error reading from watcher %s", err)
		}
	}
}