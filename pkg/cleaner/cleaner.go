package cleaner

import (
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"time"

	"github.com/op/go-logging"
)

var logger = logging.MustGetLogger("cleaner")

func NewCleaner(cleanInterval time.Duration, deploymentColumnName string, removeChannel chan<- string) *Cleaner {
	return &Cleaner{
		cleanInterval:        cleanInterval,
		deploymentColumnName: deploymentColumnName,
		lastSeenMap:          make(map[string]time.Time, 0),
		lastSeenChannel:      make(chan string),
		removeChannel:        removeChannel,
	}
}

type Cleaner struct {
	cleanInterval        time.Duration
	deploymentColumnName string
	lastSeenMap          map[string]time.Time
	lastSeenChannel      chan string
	removeChannel        chan<- string
}

func (c *Cleaner) Run() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.periodicClean()
		case deploy := <-c.lastSeenChannel:
			c.lastSeenMap[deploy] = time.Now()
		}
	}
}

func (c *Cleaner) periodicClean() {
	for deploy, lastSeen := range c.lastSeenMap {
		threshold := time.Now().Add(-c.cleanInterval)
		if lastSeen.Before(threshold) {
			logger.Infof("About to remove stale deploy %s", deploy)
			c.removeChannel <- deploy
		}
	}
}

func (c *Cleaner) OnRow(row tablewatch.Row) {
	deploy, ok := row[c.deploymentColumnName]
	if !ok {
		logger.Warningf("Column %s not found on row %v", c.deploymentColumnName, row)
		return
	}

	c.lastSeenChannel <- deploy
}

func (c *Cleaner) OnDeploy(deploy string) {
	c.lastSeenChannel <- deploy
}
