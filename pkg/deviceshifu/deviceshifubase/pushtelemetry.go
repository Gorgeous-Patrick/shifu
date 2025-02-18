package deviceshifubase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/edgenesis/shifu/pkg/deviceshifu/utils"
	"github.com/edgenesis/shifu/pkg/k8s/api/v1alpha1"
	"github.com/edgenesis/shifu/pkg/logger"
)

func PushTelemetryCollectionService(tss *v1alpha1.TelemetryServiceSpec, message *http.Response) error {
	if tss.ServiceSettings == nil {
		return fmt.Errorf("empty telemetryServiceSpec")
	}

	if tss.ServiceSettings.HTTPSetting != nil {
		err := pushToHTTPTelemetryCollectionService(message, *tss.TelemetrySeriveEndpoint)
		if err != nil {
			return err
		}
	}

	if tss.ServiceSettings.MQTTSetting != nil {
		request := &v1alpha1.TelemetryRequest{
			MQTTSetting: tss.ServiceSettings.MQTTSetting,
		}
		telemetryServicePath := *tss.TelemetrySeriveEndpoint + v1alpha1.TelemetryServiceURIMQTT
		err := pushToShifuTelemetryCollectionService(message, request, telemetryServicePath)
		if err != nil {
			return err
		}
	}

	if tss.ServiceSettings.SQLSetting != nil {
		request := &v1alpha1.TelemetryRequest{
			SQLConnectionSetting: tss.ServiceSettings.SQLSetting,
		}
		telemetryServicePath := *tss.TelemetrySeriveEndpoint + v1alpha1.TelemetryServiceURIMQTT
		err := pushToShifuTelemetryCollectionService(message, request, telemetryServicePath)
		if err != nil {
			return err
		}
	}

	return nil
}

// PushToHTTPTelemetryCollectionService push telemetry data to Collection Service
func pushToHTTPTelemetryCollectionService(message *http.Response, telemetryCollectionService string) error {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(DeviceTelemetryTimeoutInMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telemetryCollectionService, message.Body)
	if err != nil {
		logger.Errorf("error creating request for telemetry service, error: %v" + err.Error())
		return err
	}

	logger.Infof("pushing %v to %v", message.Body, telemetryCollectionService)
	utils.CopyHeader(req.Header, req.Header)
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf("HTTP POST error for telemetry service %v, error: %v", telemetryCollectionService, err.Error())
		return err
	}
	return nil
}

func pushToShifuTelemetryCollectionService(message *http.Response, request *v1alpha1.TelemetryRequest, targetServerAddress string) error {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Duration(DeviceTelemetryTimeoutInMS)*time.Millisecond)
	defer cancel()

	rawData, err := io.ReadAll(message.Body)
	if err != nil {
		logger.Errorf("Error when Read Info From RequestBody, error: %v", err)
		return err
	}

	request.RawData = rawData
	requestBody, err := json.Marshal(request)
	if err != nil {
		logger.Errorf("Error when marshal request to []byte, error: %v", err)
		return err
	}
	logger.Infof("requestBody is %s", string(requestBody))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetServerAddress, bytes.NewBuffer(requestBody))
	if err != nil {
		logger.Errorf("Error when build request with requestBody, error: %v", err)
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf("Error when send request to Server, error: %v", err)
		return err
	}
	logger.Infof("successfully sent message %v to telemetry service address %v", string(rawData), targetServerAddress)
	err = resp.Body.Close()
	if err != nil {
		logger.Errorf("Error when Close response Body, error: %v", err)
		return err
	}

	return nil
}

func getTelemetryCollectionServiceMap(ds *DeviceShifuBase) (map[string]v1alpha1.TelemetryServiceSpec, error) {
	serviceAddressCache := make(map[string]v1alpha1.TelemetryServiceSpec)
	res := make(map[string]v1alpha1.TelemetryServiceSpec)
	defaultPushToServer := false
	defaultTelemetryCollectionService := ""
	defaultTelemetryServiceAddress := ""
	defaultTelemetryServiceSpec := &v1alpha1.TelemetryServiceSpec{
		TelemetrySeriveEndpoint: &defaultTelemetryServiceAddress,
	}

	telemetries := ds.DeviceShifuConfig.Telemetries
	if telemetries == nil {
		return res, nil
	}

	if telemetries.DeviceShifuTelemetrySettings == nil {
		telemetries.DeviceShifuTelemetrySettings = &DeviceShifuTelemetrySettings{}
	}

	if telemetries.DeviceShifuTelemetrySettings.DeviceShifuTelemetryDefaultPushToServer != nil {
		defaultPushToServer = *telemetries.DeviceShifuTelemetrySettings.DeviceShifuTelemetryDefaultPushToServer
	}

	if defaultPushToServer {
		if telemetries.DeviceShifuTelemetrySettings.DeviceShifuTelemetryDefaultCollectionService == nil ||
			len(*telemetries.DeviceShifuTelemetrySettings.DeviceShifuTelemetryDefaultCollectionService) == 0 {
			return nil, fmt.Errorf("you need to configure defaultTelemetryCollectionService if setting defaultPushToServer to true")

		}

		defaultTelemetryCollectionService = *telemetries.DeviceShifuTelemetrySettings.DeviceShifuTelemetryDefaultCollectionService
		var telemetryService v1alpha1.TelemetryService
		if err := ds.RestClient.Get().
			Namespace(ds.EdgeDevice.Namespace).
			Resource(TelemetryCollectionServiceResourceStr).
			Name(defaultTelemetryCollectionService).
			Do(context.TODO()).
			Into(&telemetryService); err != nil {
			logger.Errorf("unable to get telemetry service %v, error: %v", defaultTelemetryCollectionService, err)
		}

		serviceAddressCache[defaultTelemetryCollectionService] = telemetryService.Spec
	}

	for telemetryName, telemetry := range telemetries.DeviceShifuTelemetries {
		if telemetry == nil {
			continue
		}

		pushSettings := telemetry.DeviceShifuTelemetryProperties.PushSettings
		if pushSettings == nil {
			res[telemetryName] = *defaultTelemetryServiceSpec
			continue
		}

		if pushSettings.DeviceShifuTelemetryPushToServer != nil {
			if !*pushSettings.DeviceShifuTelemetryPushToServer {
				continue
			}
		}

		if pushSettings.DeviceShifuTelemetryCollectionService != nil &&
			len(*pushSettings.DeviceShifuTelemetryCollectionService) != 0 {
			if telemetryServiceAddress, exist := serviceAddressCache[*pushSettings.DeviceShifuTelemetryCollectionService]; exist {
				res[telemetryName] = telemetryServiceAddress
				continue
			}

			var telemetryService v1alpha1.TelemetryService
			if err := ds.RestClient.Get().
				Namespace(ds.EdgeDevice.Namespace).
				Resource(TelemetryCollectionServiceResourceStr).
				Name(*pushSettings.DeviceShifuTelemetryCollectionService).
				Do(context.TODO()).
				Into(&telemetryService); err != nil {
				logger.Errorf("unable to get telemetry service %v, error: %v", *pushSettings.DeviceShifuTelemetryCollectionService, err)
				continue
			}

			serviceAddressCache[*pushSettings.DeviceShifuTelemetryCollectionService] = telemetryService.Spec
			res[telemetryName] = telemetryService.Spec
			continue
		}

		res[telemetryName] = *defaultTelemetryServiceSpec
	}

	return res, nil
}
