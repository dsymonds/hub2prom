# hub2prom

This is a bridge between [Hubitat](https://hubitat.com/) and [Prometheus](https://prometheus.io/).
It scrapes device information and status from a Hubitat using the Maker API,
and exports it in a Prometheus-compatible format.

## Setup

First, enable the [Maker API](https://docs2.hubitat.com/apps/maker-api) on your
Hubitat. This involves adding a built-in app called "Maker API". From its
configuration, you probably want to "Allow Access via Local IP Address" so that
hub2prom can use it. Then select the devices whose information you want
exported through the Maker API; every selected device with attributes (sensor
values) will be exported through `hub2prom`.

Next, create a `hub2prom.yaml` file that looks like this:

```yaml
maker_api: http://1.2.3.4/apps/api/3/devices
access_token: 123abc-4567-8902-cafed00d

metrics:
  - temperature
  - humidity
  - battery
  - illuminance
  - motion
  - contact
```

The `maker_api` address should be the prefix of the URLs that the Maker API
gives as examples (up to and including the `/devices` path component). The
`access_token` value should be what it is reporting too.

`metrics` is a list of the attributes that you care about. The full list above
is supported, but you can reduce the list if you want to be more selective.
Each will be the name of a Prometheus gauge metric exported by `hub2prom`,
including `name`, `label` and `room` metric labels.

Run `hub2prom` as you see fit (e.g. using `systemd`), using its `-port` flag
to specify a HTTP port for it to listen on.

Finally, configure Prometheus to scrape the aforementioned HTTP port with the
standard `/metrics` path. Each scrape will cause `hub2prom` to fetch all the
device information from the Maker API.

## Debugging

If metrics don't seem to be flowing, use the `/debug/requests` endpoint on
`hub2prom` in the browser to see its traces for each hit of the `/metrics`
endpoint.
