package tablewatch

import (
	"database/sql"
	"fmt"
	"os"
	"path"
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

func (d *dbConn) addSymlinkChainToWatch(dirs map[string]map[string]bool, filePath string) {
	const maxSymlinkDepth = 10 // Prevent infinite loops

	// Start with the original file
	currentPath := filePath

	for depth := 0; depth < maxSymlinkDepth; depth++ {
		// Add the current file to the watch map
		dir := path.Dir(currentPath)
		name := path.Base(currentPath)

		if dirs[dir] == nil {
			dirs[dir] = make(map[string]bool)
		}
		dirs[dir][name] = true

		// Check if current path is a symlink
		fileInfo, err := os.Lstat(currentPath)
		if err != nil {
			logger.Debugf("Failed to stat %s: %s", currentPath, err)
			break
		}

		// If it's not a symlink, we're done
		if fileInfo.Mode()&os.ModeSymlink == 0 {
			break
		}

		// Read the symlink target (step by step, not full resolution)
		targetPath, err := os.Readlink(currentPath)
		if err != nil {
			logger.Debugf("Failed to read symlink %s: %s", currentPath, err)
			break
		}

		// Make the target path absolute if it's relative
		if !path.IsAbs(targetPath) {
			targetPath = path.Join(path.Dir(currentPath), targetPath)
		}

		// If we got the same path, we've hit a circular symlink
		if targetPath == currentPath {
			logger.Warningf("Circular symlink detected at %s", currentPath)
			break
		}

		currentPath = targetPath
		logger.Debugf("Added symlink level %d to watch: %s -> %s", depth+1, filePath, currentPath)
	}
}

func (d *dbConn) getDirsToWatch() map[string]map[string]bool {
	dirs := make(map[string]map[string]bool)

	if d.passwordFile != "" {
		d.addSymlinkChainToWatch(dirs, d.passwordFile)
	}

	if d.usernameFile != "" {
		d.addSymlinkChainToWatch(dirs, d.usernameFile)
	}

	return dirs
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

	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			logger.Errorf("Unable to watch for %s %s", dir, err)
			return
		}
	}

	logger.Debugf("Start watching for DB credential files (dirs: %v)", dirs)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			dir := path.Dir(event.Name)
			name := path.Base(event.Name)

			logger.Debugf("File Changed on the Filesystem [dir: %s] [name: %s] [operation? %s]", dir, name, event.Op)

			if event.Has(fsnotify.Remove) {
				continue
			}

			files, ok := dirs[dir]
			if !ok {
				continue
			}

			_, ok = files[name]
			if !ok {
				continue
			}

			logger.Infof("Relading DB credentials file: %s", event.Name)
			if err := d.openAndVerify(); err != nil {
				logger.Errorf("Error openning db connection during rotation %s", err)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Infof("Error reading from watcher %s", err)
		}
	}
}
