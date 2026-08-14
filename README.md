# sing-canokey

Virtual canokey devices, for Linux, macOS, and Windows.

## Usage

Create a virtual device directly on the local machine:

```shell
# Supports Linux (ROOT), Windows (administrator), and macOS (ROOT, requires SIP to be disabled).

sing-canokey local [--file canokey-file] [--id canokey] [--touch]
```

Provide a virtual device to a sing-box dynamic USB/IP server:

```shell
sing-canokey provide [--url sing-box API URL] [--secret sing-box API secret] [--server custom usbip-server tag] [--file canokey-file] [--id canokey] [--touch]
```

Provide a virtual devices as a standalone USB/IP server:

```shell
sing-canokey server [--listen server:port] [--file canokey-file] [--id canokey] [--touch]
```