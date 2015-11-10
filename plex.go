package goplex

import (
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"
	"sync"
)

type PlexUser struct {
	AuthToken string
}

type InvalidHttpStatusCode struct {
	HttpStatus int
}

func (e *InvalidHttpStatusCode) Error() string {
	return fmt.Sprintf("Invalid plex credentials: HTTP=%d", e.HttpStatus)
}

func newPlexRequest(method, url, authToken string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequest(
		method,
		url,
		body,
	)
	if err != nil {
		return nil, err
	}

	request.Header.Set("X-Plex-Platform", "golang")
	request.Header.Set("X-Plex-Platform-Version", "0.0")
	request.Header.Set("X-Plex-Provides", "player,controller")
	request.Header.Set("X-Plex-Version", "0.0")
	request.Header.Set("X-Plex-Device", "platform")
	request.Header.Set("X-Plex-Client-Identifier", "identifier")

	if len(authToken) > 0 {
		request.Header.Add("X-Plex-Token", authToken)
	}

	return request, nil
}

func getResponse(request *http.Request, statusCodes ...int) (*http.Response, error) {
	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	for _, statusCode := range statusCodes {
		if response.StatusCode == statusCode {
			return response, nil
		}
	}

	return nil, &InvalidHttpStatusCode{
		HttpStatus: response.StatusCode,
	}
}

func unmarshalResponse(response *http.Response, v interface{}) error {
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	return xml.Unmarshal(body, &v)
}

type UserAuthQuery struct {
	AuthToken 	string 	`xml:"authenticationToken,attr"`
	Email		string	`xml:"email,attr"`
	UserId		int		`xml:"id,attr"`
}

func SignIn(username, password string) (*UserAuthQuery, error) {
	request, err := newPlexRequest(
		"POST",
		"https://my.plexapp.com/users/sign_in.xml",
		"",
		nil,
	)
	if err != nil {
		return nil, err
	}

	request.SetBasicAuth(username, password)

	response, err := getResponse(request, http.StatusCreated)
	if response != nil {defer response.Body.Close()}
	if err != nil {
		return nil, err
	}

	var q UserAuthQuery
	err = unmarshalResponse(response, &q)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

type PlexDeviceConnection struct {
	Protocol				string	`xml:"protocol,attr"`
	Address					string	`xml:"address,attr"`
	Port					string 	`xml:"port,attr"`
	Uri						string	`xml:"uri,attr"`
	IsLocal					bool	`xml:"local,attr"`
}

func (connection *PlexDeviceConnection) Validate () bool {
	response, err := http.Get(connection.Uri)
	if response != nil {
		defer response.Body.Close()
	}

	if err != nil {
		return false
	} else {
		return true
	}
}

type PlexDevice struct {
	Name					string 	`xml:"name,attr"`
	Product					string	`xml:"product,attr"`
	ProductVersion			string	`xml:"productVersion,attr"`
	Platform				string	`xml:"platform,attr"`
	CreatedAt				uint64	`xml:"createdAt,attr"`
	ClientIdentifier		string	`xml:"clientIdentifier,attr"`
	Provides				string	`xml:"provides,attr"`
	IsOwned					bool	`xml:"owned,attr"`
	IsHttpsRequired			bool	`xml:"httpsRequired,attr"`
	IsSynced				bool	`xml:"synced,attr"`
	HasPublicAddressMatches	bool	`xml:"publicAddressMatches,attr"`
	IsOnline				bool	`xml:"presence,attr"`

	Connections				[]*PlexDeviceConnection	`xml:"Connection"`
}

type NoValidConnection struct {}
func (*NoValidConnection) Error() string { return "No valid connection found." }

func (device *PlexDevice) GetBestConnection(connectTimeout time.Duration) (*PlexDeviceConnection, error) {
	cxns := make(chan *PlexDeviceConnection)

	var connectionAttempts sync.WaitGroup

	for _, c := range device.Connections {
		connectionAttempts.Add(1)
		go func (cxn *PlexDeviceConnection) {
			defer connectionAttempts.Done()

			result := cxn.Validate()
			if result {
				cxns <- cxn
			}
		} (c)
	}

	go func (wg *sync.WaitGroup) {
		wg.Wait()
		close(cxns)
	}(&connectionAttempts)

	timeout := time.After(connectTimeout)
	for {
		select {
		case cxn := <-cxns:
			return cxn, nil
		case <- timeout:
			return nil, &NoValidConnection{}
		}
	}
}

type PlexResourceContainer struct {
	Devices		[]*PlexDevice	`xml:"Device"`
}

func (user *UserAuthQuery) Devices() ([]*PlexDevice, error) {
	request, err := newPlexRequest(
		"GET",
		"https://plex.tv/api/resources?includeHttps=1",
		user.AuthToken,
		nil,
	)
	if err != nil {
		return nil, err
	}

	response, err := getResponse(request, http.StatusOK)
	if err != nil {
		return nil, err
	}

	var q PlexResourceContainer;
	err = unmarshalResponse(response, &q)
	if err != nil {
		return nil, err
	}

	return q.Devices, nil
}
