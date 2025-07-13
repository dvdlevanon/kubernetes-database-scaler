package tablewatch

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

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

	logger.Debugf("Reading DB username from file %s", d.usernameFile)

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

	logger.Debugf("Reading DB password from file %s", d.passwordFile)

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

	dsn := fmt.Sprintf("host=%s port=%s dbname=%s",
		d.host, d.port, d.dbname)

	if username != "" {
		dsn = fmt.Sprintf("%s user=%s", dsn, username)
	}

	if password != "" {
		dsn = fmt.Sprintf("%s password=%s", dsn, password)
	}

	return dsn, nil
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
	oldConn := d.conn
	conn, err := d.openDbConnection()
	if err != nil {
		return err
	}

	d.conn = conn
	result := d.verifyDbConnection()

	if oldConn != nil {
		oldConn.Close()
	}

	return result
}

func (d *dbConn) getDirsToWatch() map[string]bool {
	dirs := make(map[string]bool)

	if d.passwordFile != "" {
		dir := path.Dir(d.passwordFile)
		dirs[dir] = true
	}

	if d.usernameFile != "" {
		dir := path.Dir(d.usernameFile)
		dirs[dir] = true
	}

	return dirs
}

func (d *dbConn) getCurrentCredentials() (string, string, error) {
	username, err := d.getUsername()
	if err != nil {
		return "", "", err
	}

	password, err := d.getPassword()
	if err != nil {
		return "", "", err
	}

	return username, password, nil
}

func (d *dbConn) watchDbCredentials() {
	dirs := d.getDirsToWatch()
	if len(dirs) == 0 {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorf("Error initializing watcher %s", err)
		return
	}
	defer watcher.Close()

	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			logger.Errorf("Unable to watch directory %s: %s", dir, err)
			return
		}
	}

	logger.Debugf("Start watching for DB credential changes in directories: %v", dirs)

	// Get initial credentials
	currentUsername, currentPassword, err := d.getCurrentCredentials()
	if err != nil {
		logger.Errorf("Failed to get initial credentials: %s", err)
		return
	}

	const reloadDelay = 1 * time.Second
	var reloadTimer *time.Timer
	var reloadChan <-chan time.Time

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			dir := path.Dir(event.Name)
			name := path.Base(event.Name)

			logger.Debugf("File Changed on the Filesystem [dir: %s] [name: %s] [operation: %s]", dir, name, event.Op)

			// Check if this event is in one of our watched directories
			if _, ok := dirs[dir]; !ok {
				continue
			}

			// Reset/start timer on any file system event
			if reloadTimer != nil {
				reloadTimer.Stop()
			}
			reloadTimer = time.NewTimer(reloadDelay)
			reloadChan = reloadTimer.C

		case <-reloadChan:
			// Timer expired, check if credentials actually changed
			newUsername, newPassword, err := d.getCurrentCredentials()
			if err != nil {
				logger.Errorf("Failed to read credentials during reload check: %s", err)
				continue
			}

			if newUsername != currentUsername || newPassword != currentPassword {
				logger.Infof("Credentials changed, reloading DB connection")
				if err := d.openAndVerify(); err != nil {
					logger.Errorf("Error opening db connection during rotation: %s", err)
				} else {
					logger.Infof("Successfully reloaded DB credentials")
					currentUsername = newUsername
					currentPassword = newPassword
				}
			} else {
				logger.Debugf("File system event detected but credentials unchanged, skipping reload")
			}

			// Clear the channel to prevent it from being selected again
			reloadChan = nil

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Errorf("Error reading from watcher: %s", err)
		}
	}
}
