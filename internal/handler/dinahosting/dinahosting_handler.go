package dinahosting

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TimothyYe/godns/internal/settings"
	"github.com/TimothyYe/godns/internal/utils"
	"github.com/TimothyYe/godns/pkg/notification"

	log "github.com/sirupsen/logrus"
)

var (
	// DinahostingUrl the API address for NoIP
	DinahostingUrl = "https://dinahosting.com/special/api.php?command=Domain_Zone_UpdateTypeA&domain=%s&hostname=%s&ip=%s&responseType=Json"
)

// Handler struct
type Handler struct {
	Configuration *settings.Settings
}

// SetConfiguration pass dns settings and store it to handler instance
func (handler *Handler) SetConfiguration(conf *settings.Settings) {
	handler.Configuration = conf
}

// DomainLoop the main logic loop
func (handler *Handler) DomainLoop(domain *settings.Domain, panicChan chan<- settings.Domain, runOnce bool) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Recovered in %v: %v", err, string(debug.Stack()))
			panicChan <- *domain
		}
	}()

	looping := false

	for while := true; while; while = !runOnce {
		if looping {
			// Sleep with interval
			log.Debugf("Going to sleep, will start next checking in %d seconds...", handler.Configuration.Interval)
			time.Sleep(time.Second * time.Duration(handler.Configuration.Interval))
		}

		looping = true
		currentIP, err := utils.GetCurrentIP(handler.Configuration)

		if err != nil {
			log.Error("get_currentIP:", err)
			continue
		}

		log.Debug("currentIP is:", currentIP)
		client := utils.GetHttpClient(handler.Configuration, handler.Configuration.UseProxy)

		for _, subDomain := range domain.SubDomains {
			hostname := subDomain + "." + domain.DomainName
			lastIP, err := utils.ResolveDNS(hostname, handler.Configuration.Resolver, handler.Configuration.IPType)
			if err != nil {
				log.Error(err)
				continue
			}

			//check against currently known IP, if no change, skip update
			if currentIP == lastIP {
				log.Infof("IP is the same as cached one (%s). Skip update.", currentIP)
			} else {
				u := fmt.Sprintf(
					DinahostingUrl,
					url.QueryEscape(domain.DomainName),
					url.QueryEscape(subDomain),
					url.QueryEscape(currentIP))
				req, err := http.NewRequest("POST", u, nil)
				if err != nil {
					log.Error("Failed to update sub domain:", subDomain)
					continue
				}

				req.Header.Add("Content-Type", "application/json")
				if handler.Configuration.UserAgent != "" {
					req.Header.Add("User-Agent", handler.Configuration.UserAgent)
				}

				// add basic auth
				auth := base64.StdEncoding.EncodeToString([]byte(handler.Configuration.Email + ":" + handler.Configuration.Password))
				req.Header.Add("Authorization", "Basic "+auth)

				// update IP with HTTP GET request
				resp, err := client.Do(req)
				if err != nil {
					// handle error
					log.Error("Failed to update sub domain:", subDomain)
					continue
				}

				defer resp.Body.Close()

				body, err := ioutil.ReadAll(resp.Body)
				if err != nil || !strings.Contains(string(body), `"responseCode":1000`) {
					log.Error(resp.Status)
					log.Error(string(body))
					log.Error("Failed to update the IP. ", err)
					continue
				} else {
					log.Infof("IP updated to: %s", currentIP)
				}

				// Send notification
				notification.GetNotificationManager(handler.Configuration).Send(fmt.Sprintf("%s.%s", subDomain, domain.DomainName), currentIP)
			}
		}
	}
}
