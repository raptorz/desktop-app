package api

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

type Client struct {
	client  *resty.Client
	baseURL string
	token   string
	version string
	macAddr string
}

func NewClient() *Client {
	return &Client{
		client:  resty.New().SetTimeout(60 * time.Second),
		version: "linux_amd64_2.0",
	}
}

func (c *Client) SetToken(token string) {
	c.token = token
}

func (c *Client) SetHost(host string) {
	c.baseURL = host
	c.client.SetBaseURL(host + "/api")
}

func (c *Client) SetMacAddr(addr string) {
	c.macAddr = addr
}

func (c *Client) GetBaseURL() string {
	return c.baseURL
}

func (c *Client) url(path string) string {
	return fmt.Sprintf("%s/api/%s", c.baseURL, path)
}

func (c *Client) commonParams() map[string]string {
	params := map[string]string{
		"token": c.token,
	}
	if c.macAddr != "" {
		params["macAddr"] = c.macAddr
	}
	if c.version != "" {
		params["v"] = c.version
	}
	return params
}

func (c *Client) get(path string, params map[string]string) (*resty.Response, error) {
	allParams := c.commonParams()
	for k, v := range params {
		allParams[k] = v
	}

	resp, err := c.client.R().SetQueryParams(allParams).Get(path)
	if err != nil {
		logrus.Errorf("API GET %s error: %v", path, err)
		return nil, err
	}

	logrus.Debugf("API GET %s status: %d", path, resp.StatusCode())
	return resp, nil
}

func (c *Client) post(path string, data interface{}, params map[string]string) (*resty.Response, error) {
	allParams := c.commonParams()
	for k, v := range params {
		allParams[k] = v
	}

	resp, err := c.client.R().
		SetQueryParams(allParams).
		SetBody(data).
		Post(path)
	if err != nil {
		logrus.Errorf("API POST %s error: %v", path, err)
		return nil, err
	}

	logrus.Debugf("API POST %s status: %d", path, resp.StatusCode())
	return resp, nil
}

func (c *Client) postFiles(path string, formData map[string]string, files map[string]string) (*resty.Response, error) {
	allParams := c.commonParams()
	for k, v := range formData {
		allParams[k] = v
	}

	req := c.client.R().SetQueryParams(allParams)

	for fieldName, filePath := range files {
		req = req.SetFile(fieldName, filePath)
	}

	resp, err := req.Post(path)
	if err != nil {
		logrus.Errorf("API POST files %s error: %v", path, err)
		return nil, err
	}

	logrus.Debugf("API POST files %s status: %d", path, resp.StatusCode())
	return resp, nil
}
