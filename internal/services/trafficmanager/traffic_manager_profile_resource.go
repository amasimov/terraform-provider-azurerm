package trafficmanager

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/trafficmanager/mgmt/2018-08-01/trafficmanager"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/tf"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/trafficmanager/parse"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/trafficmanager/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tags"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/suppress"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/internal/timeouts"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

func resourceArmTrafficManagerProfile() *pluginsdk.Resource {
	return &pluginsdk.Resource{
		Create: resourceArmTrafficManagerProfileCreate,
		Read:   resourceArmTrafficManagerProfileRead,
		Update: resourceArmTrafficManagerProfileUpdate,
		Delete: resourceArmTrafficManagerProfileDelete,
		// TODO: replace this with an importer which validates the ID during import
		Importer: pluginsdk.DefaultImporter(),

		Timeouts: &pluginsdk.ResourceTimeout{
			Create: pluginsdk.DefaultTimeout(30 * time.Minute),
			Read:   pluginsdk.DefaultTimeout(5 * time.Minute),
			Update: pluginsdk.DefaultTimeout(30 * time.Minute),
			Delete: pluginsdk.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*pluginsdk.Schema{
			"name": {
				Type:         pluginsdk.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringIsNotEmpty,
			},

			"resource_group_name": azure.SchemaResourceGroupNameDiffSuppress(),

			"profile_status": {
				Type:     pluginsdk.TypeString,
				Optional: true,
				Computed: true,
				ValidateFunc: validation.StringInSlice([]string{
					string(trafficmanager.ProfileStatusEnabled),
					string(trafficmanager.ProfileStatusDisabled),
				}, true),
				DiffSuppressFunc: suppress.CaseDifference,
			},

			"traffic_routing_method": {
				Type:     pluginsdk.TypeString,
				Required: true,
				ValidateFunc: validation.StringInSlice([]string{
					string(trafficmanager.TrafficRoutingMethodGeographic),
					string(trafficmanager.TrafficRoutingMethodWeighted),
					string(trafficmanager.TrafficRoutingMethodPerformance),
					string(trafficmanager.TrafficRoutingMethodPriority),
					string(trafficmanager.TrafficRoutingMethodSubnet),
					string(trafficmanager.TrafficRoutingMethodMultiValue),
				}, false),
			},

			"dns_config": {
				Type:     pluginsdk.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"relative_name": {
							Type:     pluginsdk.TypeString,
							ForceNew: true,
							Required: true,
						},
						"ttl": {
							Type:         pluginsdk.TypeInt,
							Required:     true,
							ValidateFunc: validation.IntBetween(0, 2147483647),
						},
					},
				},
			},

			"monitor_config": {
				Type:     pluginsdk.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &pluginsdk.Resource{
					Schema: map[string]*pluginsdk.Schema{
						"expected_status_code_ranges": {
							Type:     pluginsdk.TypeList,
							Optional: true,
							Elem: &pluginsdk.Schema{
								Type:         pluginsdk.TypeString,
								ValidateFunc: validate.StatusCodeRange,
							},
						},

						"custom_header": {
							Type:     pluginsdk.TypeList,
							Optional: true,
							Elem: &pluginsdk.Resource{
								Schema: map[string]*pluginsdk.Schema{
									"name": {
										Type:         pluginsdk.TypeString,
										Required:     true,
										ValidateFunc: validation.StringIsNotEmpty,
									},
									"value": {
										Type:     pluginsdk.TypeString,
										Required: true,
									},
								},
							},
						},

						"protocol": {
							Type:     pluginsdk.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(trafficmanager.MonitorProtocolHTTP),
								string(trafficmanager.MonitorProtocolHTTPS),
								string(trafficmanager.MonitorProtocolTCP),
							}, true),
							DiffSuppressFunc: suppress.CaseDifference,
						},

						"port": {
							Type:         pluginsdk.TypeInt,
							Required:     true,
							ValidateFunc: validation.IntBetween(1, 65535),
						},

						"path": {
							Type:     pluginsdk.TypeString,
							Optional: true,
						},

						"interval_in_seconds": {
							Type:         pluginsdk.TypeInt,
							Optional:     true,
							ValidateFunc: validation.IntInSlice([]int{10, 30}),
							Default:      30,
						},

						"timeout_in_seconds": {
							Type:         pluginsdk.TypeInt,
							Optional:     true,
							ValidateFunc: validation.IntBetween(5, 10),
							Default:      10,
						},

						"tolerated_number_of_failures": {
							Type:         pluginsdk.TypeInt,
							Optional:     true,
							ValidateFunc: validation.IntBetween(0, 9),
							Default:      3,
						},
					},
				},
			},

			"fqdn": {
				Type:     pluginsdk.TypeString,
				Computed: true,
			},

			"max_return": {
				Type:         pluginsdk.TypeInt,
				Optional:     true,
				ValidateFunc: validation.IntBetween(1, 8),
			},

			"traffic_view_enabled": {
				Type:     pluginsdk.TypeBool,
				Optional: true,
			},

			"tags": tags.Schema(),
		},
	}
}

func resourceArmTrafficManagerProfileCreate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).TrafficManager.ProfilesClient
	subscriptionId := meta.(*clients.Client).Account.SubscriptionId
	ctx, cancel := timeouts.ForCreate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	log.Printf("[INFO] preparing arguments for Traffic Manager Profile creation.")

	resourceId := parse.NewTrafficManagerProfileID(subscriptionId, d.Get("resource_group_name").(string), d.Get("name").(string))
	existing, err := client.Get(ctx, resourceId.ResourceGroup, resourceId.Name)
	if err != nil {
		if !utils.ResponseWasNotFound(existing.Response) {
			return fmt.Errorf("checking for presence of existing Traffic Manager Profile %q (Resource Group %q)", resourceId.Name, resourceId.ResourceGroup)
		}
	}

	if !utils.ResponseWasNotFound(existing.Response) {
		return tf.ImportAsExistsError("azurerm_traffic_manager_profile", resourceId.ID())
	}

	// No existing profile - start from a new struct.
	profile := trafficmanager.Profile{
		Name:     utils.String(resourceId.Name),
		Location: utils.String("global"), // must be provided in request
		ProfileProperties: &trafficmanager.ProfileProperties{
			TrafficRoutingMethod: trafficmanager.TrafficRoutingMethod(d.Get("traffic_routing_method").(string)),
			DNSConfig:            expandArmTrafficManagerDNSConfig(d),
			MonitorConfig:        expandArmTrafficManagerMonitorConfig(d),
		},
		Tags: tags.Expand(d.Get("tags").(map[string]interface{})),
	}

	if maxReturn, ok := d.GetOk("max_return"); ok {
		profile.MaxReturn = utils.Int64(int64(maxReturn.(int)))
	}

	if status, ok := d.GetOk("profile_status"); ok {
		profile.ProfileStatus = trafficmanager.ProfileStatus(status.(string))
	}

	if trafficViewStatus, ok := d.GetOk("traffic_view_enabled"); ok {
		profile.TrafficViewEnrollmentStatus = expandArmTrafficManagerTrafficView(trafficViewStatus.(bool))
	}

	if profile.ProfileProperties.TrafficRoutingMethod == trafficmanager.TrafficRoutingMethodMultiValue &&
		profile.ProfileProperties.MaxReturn == nil {
		return fmt.Errorf("`max_return` must be specified when `traffic_routing_method` is set to `MultiValue`")
	}

	if *profile.ProfileProperties.MonitorConfig.IntervalInSeconds == int64(10) &&
		*profile.ProfileProperties.MonitorConfig.TimeoutInSeconds == int64(10) {
		return fmt.Errorf("`timeout_in_seconds` must be between `5` and `9` when `interval_in_seconds` is set to `10`")
	}

	if _, err := client.CreateOrUpdate(ctx, resourceId.ResourceGroup, resourceId.Name, profile); err != nil {
		return fmt.Errorf("creating Traffic Manager Profile %q (Resource Group %q): %+v", resourceId.Name, resourceId.ResourceGroup, err)
	}

	d.SetId(resourceId.ID())
	return resourceArmTrafficManagerProfileRead(d, meta)
}

func resourceArmTrafficManagerProfileRead(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).TrafficManager.ProfilesClient
	ctx, cancel := timeouts.ForRead(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.TrafficManagerProfileID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Get(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("retrieving Traffic Manager Profile %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	d.Set("name", id.Name)
	d.Set("resource_group_name", id.ResourceGroup)

	if profile := resp.ProfileProperties; profile != nil {
		d.Set("profile_status", profile.ProfileStatus)
		d.Set("traffic_routing_method", profile.TrafficRoutingMethod)
		d.Set("max_return", profile.MaxReturn)

		d.Set("dns_config", flattenAzureRMTrafficManagerProfileDNSConfig(profile.DNSConfig))
		d.Set("monitor_config", flattenAzureRMTrafficManagerProfileMonitorConfig(profile.MonitorConfig))
		d.Set("traffic_view_enabled", profile.TrafficViewEnrollmentStatus == trafficmanager.TrafficViewEnrollmentStatusEnabled)

		// fqdn is actually inside DNSConfig, inlined for simpler reference
		if dns := profile.DNSConfig; dns != nil {
			d.Set("fqdn", dns.Fqdn)
		}
	}
	return tags.FlattenAndSet(d, resp.Tags)
}

func resourceArmTrafficManagerProfileUpdate(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).TrafficManager.ProfilesClient
	ctx, cancel := timeouts.ForUpdate(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.TrafficManagerProfileID(d.Id())
	if err != nil {
		return err
	}

	update := trafficmanager.Profile{
		ProfileProperties: &trafficmanager.ProfileProperties{},
	}
	if d.HasChange("tags") {
		update.Tags = tags.Expand(d.Get("tags").(map[string]interface{}))
	}

	if d.HasChange("profile_status") {
		update.ProfileProperties.ProfileStatus = trafficmanager.ProfileStatus(d.Get("profile_status").(string))
	}

	if d.HasChange("traffic_routing_method") {
		update.ProfileProperties.TrafficRoutingMethod = trafficmanager.TrafficRoutingMethod(d.Get("traffic_routing_method").(string))
	}

	if d.HasChange("max_return") {
		if maxReturn, ok := d.GetOk("max_return"); ok {
			update.MaxReturn = utils.Int64(int64(maxReturn.(int)))
		}
	}

	if d.HasChange("dns_config") {
		update.ProfileProperties.DNSConfig = expandArmTrafficManagerDNSConfig(d)
	}

	if d.HasChange("monitor_config") {
		update.ProfileProperties.MonitorConfig = expandArmTrafficManagerMonitorConfig(d)
	}

	if d.HasChange("traffic_view_enabled") {
		if trafficViewStatus, ok := d.GetOk("traffic_view_enabled"); ok {
			update.ProfileProperties.TrafficViewEnrollmentStatus = expandArmTrafficManagerTrafficView(trafficViewStatus.(bool))
		}
	}

	if _, err := client.Update(ctx, id.ResourceGroup, id.Name, update); err != nil {
		return fmt.Errorf("updating Traffic Manager Profile %q (Resource Group %q): %+v", id.Name, id.ResourceGroup, err)
	}

	return resourceArmTrafficManagerProfileRead(d, meta)
}

func resourceArmTrafficManagerProfileDelete(d *pluginsdk.ResourceData, meta interface{}) error {
	client := meta.(*clients.Client).TrafficManager.ProfilesClient
	ctx, cancel := timeouts.ForDelete(meta.(*clients.Client).StopContext, d)
	defer cancel()

	id, err := parse.TrafficManagerProfileID(d.Id())
	if err != nil {
		return err
	}

	resp, err := client.Delete(ctx, id.ResourceGroup, id.Name)
	if err != nil {
		if !utils.ResponseWasNotFound(resp.Response) {
			return err
		}
	}

	return nil
}

func expandArmTrafficManagerMonitorConfig(d *pluginsdk.ResourceData) *trafficmanager.MonitorConfig {
	monitorSets := d.Get("monitor_config").([]interface{})
	monitor := monitorSets[0].(map[string]interface{})

	customHeaders := expandArmTrafficManagerCustomHeadersConfig(monitor["custom_header"].([]interface{}))

	cfg := trafficmanager.MonitorConfig{
		Protocol:                  trafficmanager.MonitorProtocol(monitor["protocol"].(string)),
		CustomHeaders:             customHeaders,
		Port:                      utils.Int64(int64(monitor["port"].(int))),
		Path:                      utils.String(monitor["path"].(string)),
		IntervalInSeconds:         utils.Int64(int64(monitor["interval_in_seconds"].(int))),
		TimeoutInSeconds:          utils.Int64(int64(monitor["timeout_in_seconds"].(int))),
		ToleratedNumberOfFailures: utils.Int64(int64(monitor["tolerated_number_of_failures"].(int))),
	}

	if v, ok := monitor["expected_status_code_ranges"].([]interface{}); ok {
		ranges := make([]trafficmanager.MonitorConfigExpectedStatusCodeRangesItem, 0)
		for _, r := range v {
			parts := strings.Split(r.(string), "-")
			min, _ := strconv.Atoi(parts[0])
			max, _ := strconv.Atoi(parts[1])
			ranges = append(ranges, trafficmanager.MonitorConfigExpectedStatusCodeRangesItem{
				Min: utils.Int32(int32(min)),
				Max: utils.Int32(int32(max)),
			})
		}
		cfg.ExpectedStatusCodeRanges = &ranges
	}

	return &cfg
}

func expandArmTrafficManagerCustomHeadersConfig(d []interface{}) *[]trafficmanager.MonitorConfigCustomHeadersItem {
	if len(d) == 0 || d[0] == nil {
		return nil
	}

	customHeaders := make([]trafficmanager.MonitorConfigCustomHeadersItem, len(d))

	for i, v := range d {
		ch := v.(map[string]interface{})
		customHeaders[i] = trafficmanager.MonitorConfigCustomHeadersItem{
			Name:  utils.String(ch["name"].(string)),
			Value: utils.String(ch["value"].(string)),
		}
	}

	return &customHeaders
}

func flattenArmTrafficManagerCustomHeadersConfig(input *[]trafficmanager.MonitorConfigCustomHeadersItem) []interface{} {
	result := make([]interface{}, 0)
	if input == nil {
		return result
	}

	headers := *input
	if len(headers) == 0 {
		return result
	}

	for _, v := range headers {
		header := make(map[string]string, 2)
		header["name"] = *v.Name
		header["value"] = *v.Value
		result = append(result, header)
	}

	return result
}

func expandArmTrafficManagerDNSConfig(d *pluginsdk.ResourceData) *trafficmanager.DNSConfig {
	dnsSets := d.Get("dns_config").([]interface{})
	dns := dnsSets[0].(map[string]interface{})

	name := dns["relative_name"].(string)
	ttl := int64(dns["ttl"].(int))

	return &trafficmanager.DNSConfig{
		RelativeName: &name,
		TTL:          &ttl,
	}
}

func expandArmTrafficManagerTrafficView(s bool) trafficmanager.TrafficViewEnrollmentStatus {
	if s {
		return trafficmanager.TrafficViewEnrollmentStatusEnabled
	}
	return trafficmanager.TrafficViewEnrollmentStatusDisabled
}

func flattenAzureRMTrafficManagerProfileDNSConfig(dns *trafficmanager.DNSConfig) []interface{} {
	result := make(map[string]interface{})

	result["relative_name"] = *dns.RelativeName
	result["ttl"] = int(*dns.TTL)

	return []interface{}{result}
}

func flattenAzureRMTrafficManagerProfileMonitorConfig(cfg *trafficmanager.MonitorConfig) []interface{} {
	result := make(map[string]interface{})

	result["protocol"] = string(cfg.Protocol)
	result["port"] = int(*cfg.Port)
	result["custom_header"] = flattenArmTrafficManagerCustomHeadersConfig(cfg.CustomHeaders)

	if cfg.Path != nil {
		result["path"] = *cfg.Path
	}

	result["interval_in_seconds"] = int(*cfg.IntervalInSeconds)
	result["timeout_in_seconds"] = int(*cfg.TimeoutInSeconds)
	result["tolerated_number_of_failures"] = int(*cfg.ToleratedNumberOfFailures)

	if v := cfg.ExpectedStatusCodeRanges; v != nil {
		ranges := make([]string, 0)
		for _, r := range *v {
			if r.Min == nil || r.Max == nil {
				continue
			}

			ranges = append(ranges, fmt.Sprintf("%d-%d", *r.Min, *r.Max))
		}
		result["expected_status_code_ranges"] = ranges
	}

	return []interface{}{result}
}
