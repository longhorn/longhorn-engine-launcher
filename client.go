package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

type ControllerClient struct {
	controller string
}

type PortInput struct {
	Port int
}

type StartFrontendInput struct {
	Frontend string `json:"frontend"`
}

type Volume struct {
	ID       string `json:"id"`
	Frontend string `json:"frontend"`
}

type VolumeCollection struct {
	Data []Volume `json:"data"`
}

func NewControllerClient(controller string) *ControllerClient {
	if !strings.HasSuffix(controller, "/v1") {
		controller += "/v1"
	}
	return &ControllerClient{
		controller: controller,
	}
}

func (c *ControllerClient) StartFrontend(frontend string) error {
	volume, err := c.GetVolume()
	if err != nil {
		return err
	}
	return c.post("/volumes/"+volume.ID+"?action=startfrontend", StartFrontendInput{
		Frontend: frontend,
	}, nil)
}

func (c *ControllerClient) ShutdownFrontend() error {
	volume, err := c.GetVolume()
	if err != nil {
		return err
	}
	return c.post("/volumes/"+volume.ID+"?action=shutdownfrontend", struct{}{}, nil)
}

func (c *ControllerClient) UpdatePort(port int) error {
	err := c.post("/settings/updateport", &PortInput{Port: port}, nil)
	return err
}

func (c *ControllerClient) TestConnection() error {
	return c.get("/volumes", nil)
}

func (c *ControllerClient) GetVolume() (*Volume, error) {
	volumes := VolumeCollection{}
	if err := c.get("/volumes", &volumes); err != nil {
		return nil, err
	}
	return &volumes.Data[0], nil
}

func (c *ControllerClient) post(path string, req, resp interface{}) error {
	return c.do("POST", path, req, resp)
}

func (c *ControllerClient) do(method, path string, req, resp interface{}) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}

	bodyType := "application/json"
	url := path
	if !strings.HasPrefix(url, "http") {
		url = c.controller + path
	}

	logrus.Debugf("%s %s", method, url)
	httpReq, err := http.NewRequest(method, url, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", bodyType)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 300 {
		content, _ := ioutil.ReadAll(httpResp.Body)
		return fmt.Errorf("Bad response: %d %s: %s", httpResp.StatusCode, httpResp.Status, content)
	}

	if resp == nil {
		return nil
	}

	return json.NewDecoder(httpResp.Body).Decode(resp)
}

func (c *ControllerClient) get(path string, obj interface{}) error {
	resp, err := http.Get(c.controller + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if obj != nil {
		return json.NewDecoder(resp.Body).Decode(obj)
	}
	return nil
}
