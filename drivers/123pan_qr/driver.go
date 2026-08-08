package _123pan_qr

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
	"golang.org/x/time/rate"
)

// API 基地址 — yun.123pan.com（与 123Pan 驱动一致）
const qrApiBase = "https://yun.123pan.com"

var (
	qrUserInfo     = qrApiBase + "/b/api/user/info"
	qrFileList     = qrApiBase + "/api/file/list/new"
	qrDownloadInfo = qrApiBase + "/b/api/file/download_info"
	qrMkdir        = qrApiBase + "/b/api/file/upload_request"
	qrMove         = qrApiBase + "/b/api/file/mod_pid"
	qrRename       = qrApiBase + "/b/api/file/rename"
	qrTrash        = qrApiBase + "/b/api/file/trash"
)

// Pan123QR 通过扫码登录 token 访问 123 云盘。
// List/Link/MakeDir/Move/Rename/Remove 使用 yun.123pan.com 自有请求层，
// Put 委托 _123.Pan123 以支持 S3 + 新分片两种上传路径。
type Pan123QR struct {
	model.Storage
	Addition    AdditionQR
	inner       _123.Pan123
	apiRateLimit sync.Map
	token       string
}

func (d *Pan123QR) Config() driver.Config {
	return config
}

func (d *Pan123QR) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan123QR) GetRootId() string {
	return d.Addition.RootFolderID
}

func (d *Pan123QR) Init(ctx context.Context) error {
	token := strings.TrimSpace(d.Addition.Token)
	if token == "" && d.Addition.Authorization != "" {
		token = strings.TrimSpace(d.Addition.Authorization)
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "bearer ")
		d.Addition.Token = token
	}
	if token == "" {
		return fmt.Errorf("token 和 authorization 不能同时为空")
	}
	d.token = token

	// 注入到内部 _123.Pan123，Put 委托它处理两种上传路径
	d.inner.AccessToken = token
	d.inner.Platform = "web"
	d.inner.Username = "qr_user"
	d.inner.Password = "qr_user"
	d.inner.SetStorage(*d.GetStorage())

	// 验证 token 有效性
	_, err := d.request(qrUserInfo, http.MethodGet, func(req *resty.Request) {
		req.SetContext(ctx)
	}, nil)
	return err
}

func (d *Pan123QR) Drop(ctx context.Context) error { return nil }

// ---- 请求层 ----

func (d *Pan123QR) apiHeaders() map[string]string {
	return map[string]string{
		"origin":        "https://yun.123pan.com",
		"referer":       "https://yun.123pan.com/",
		"authorization": "Bearer " + d.token,
		"platform":      "android",
		"devicename":    "Xiaomi",
		"app-version":   "61",
		"x-app-version": "2.4.0",
		"user-agent":    "123pan/v2.4.0(Android_13;Xiaomi)",
	}
}

func (d *Pan123QR) request(url, method string, callback base.ReqCallback, resp any) ([]byte, error) {
	req := base.RestyClient.R()
	req.SetHeaders(d.apiHeaders())
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	res, err := req.Execute(method, url)
	if err != nil {
		return nil, err
	}
	body := res.Body()
	code := jsoniter.Get(body, "code").ToInt()
	if code != 0 {
		msg := jsoniter.Get(body, "message").ToString()
		return nil, fmt.Errorf("%s", msg)
	}
	return body, nil
}

func (d *Pan123QR) apiRateLimitWait(ctx context.Context, api string) error {
	v, _ := d.apiRateLimit.LoadOrStore(api,
		rate.NewLimiter(rate.Every(700*time.Millisecond), 1))
	return v.(*rate.Limiter).Wait(ctx)
}

// ---- List ----

func (d *Pan123QR) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	parentId := dir.GetID()
	page := 1
	res := make([]model.Obj, 0)
	for {
		if err := d.apiRateLimitWait(ctx, qrFileList); err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Next     string      `json:"Next"`
				InfoList []_123.File `json:"InfoList"`
			} `json:"data"`
		}
		_, err := d.request(qrFileList, http.MethodGet, func(req *resty.Request) {
			req.SetContext(ctx).SetQueryParams(map[string]string{
				"driveId":              "0",
				"limit":                "100",
				"next":                 "0",
				"orderBy":              "file_id",
				"orderDirection":       "desc",
				"parentFileId":         parentId,
				"trashed":              "false",
				"SearchData":           "",
				"Page":                 fmt.Sprint(page),
				"OnlyLookAbnormalFile": "0",
			})
		}, &resp)
		if err != nil {
			return nil, err
		}
		for i := range resp.Data.InfoList {
			res = append(res, &resp.Data.InfoList[i])
		}
		if len(resp.Data.InfoList) == 0 || resp.Data.Next == "-1" {
			break
		}
		page++
	}
	return res, nil
}

// ---- Link ----

func (d *Pan123QR) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	f, ok := file.(*_123.File)
	if !ok {
		ff, ok2 := file.(_123.File)
		if !ok2 {
			return nil, fmt.Errorf("can't convert obj")
		}
		f = &ff
	}
	data := base.Json{
		"driveId":   0,
		"etag":      f.Etag,
		"fileId":    f.FileId,
		"fileName":  f.FileName,
		"s3keyFlag": f.S3KeyFlag,
		"size":      f.Size,
		"type":      f.Type,
	}
	resp, err := d.request(qrDownloadInfo, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetContext(ctx)
	}, nil)
	if err != nil {
		return nil, err
	}
	downloadUrl := jsoniter.Get(resp, "data", "DownloadUrl").ToString()
	ou, _ := url.Parse(downloadUrl)
	u_ := ou.String()
	if nu := ou.Query().Get("params"); nu != "" {
		du, _ := base64.StdEncoding.DecodeString(nu)
		if u, err := url.Parse(string(du)); err == nil {
			u_ = u.String()
		}
	}
	res, err := base.NoRedirectClient.R().
		SetHeader("Referer", "https://www.123pan.com/").
		Get(u_)
	if err != nil {
		return nil, err
	}
	link := &model.Link{URL: u_}
	if res.StatusCode() == 302 {
		link.URL = res.Header().Get("location")
	} else if res.StatusCode() < 300 {
		link.URL = jsoniter.Get(res.Body(), "data", "redirect_url").ToString()
	}
	link.Header = http.Header{
		"Referer": []string{fmt.Sprintf("%s://%s/", ou.Scheme, ou.Host)},
	}
	return link, nil
}

// ---- MakeDir ----

func (d *Pan123QR) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	_, err := d.request(qrMkdir, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"driveId":      0,
			"etag":         "",
			"fileName":     dirName,
			"parentFileId": parentDir.GetID(),
			"size":         0,
			"type":         1,
		}).SetContext(ctx)
	}, nil)
	return err
}

// ---- Move ----

func (d *Pan123QR) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	_, err := d.request(qrMove, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"fileIdList":   []base.Json{{"FileId": srcObj.GetID()}},
			"parentFileId": dstDir.GetID(),
		}).SetContext(ctx)
	}, nil)
	return err
}

// ---- Rename ----

func (d *Pan123QR) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	_, err := d.request(qrRename, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"driveId":  0,
			"fileId":   srcObj.GetID(),
			"fileName": newName,
		}).SetContext(ctx)
	}, nil)
	return err
}

// ---- Copy ----

func (d *Pan123QR) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return fmt.Errorf("not supported")
}

// ---- Remove ----

func (d *Pan123QR) Remove(ctx context.Context, obj model.Obj) error {
	f, ok := obj.(*_123.File)
	if !ok {
		ff, ok2 := obj.(_123.File)
		if !ok2 {
			return fmt.Errorf("can't convert obj")
		}
		f = &ff
	}
	_, err := d.request(qrTrash, http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"driveId":           0,
			"operation":         true,
			"fileTrashInfoList": []_123.File{*f},
		}).SetContext(ctx)
	}, nil)
	return err
}

// ---- Put ----

// Put 委托给内部 _123.Pan123，它完整实现了 S3 和 newUpload 两种上传路径
func (d *Pan123QR) Put(ctx context.Context, dstDir model.Obj, file model.FileStreamer, up driver.UpdateProgress) error {
	return d.inner.Put(ctx, dstDir, file, up)
}

var _ driver.Driver = (*Pan123QR)(nil)
