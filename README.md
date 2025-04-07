# puzzmo2signal

This program sets up a minimal server to receive webhooks from [the puzzle site Puzzmo](https://www.puzzmo.com/), optionally strips the markdown formatting off them, and forwards them to a Signal group. It requires a working [signal-cli install](https://github.com/AsamK/signal-cli) and a (free) [Tailscale](https://tailscale.com/) account, which is used to make the server accessible over [Tailscale Funnel](https://tailscale.com/kb/1223/funnel).

The program requires the following environment variables to be set before running:
- `TS_HOSTNAME`: hostname for the Tailscale device and the subdomain for the URL. E.g. `puzzmo-webooks`
- `TS_AUTHKEY`: authorization key for a Tailscale device, which can be obtained from your [admin console](https://login.tailscale.com/admin/settings/keys). Should be set to reusable and ephemeral, and the member or tag that owns the device should have the Funnel `nodeAttr` [set in your ACLs](https://tailscale.com/kb/1337/acl-syntax#node-attributes)
- `SIGNAL_PHONE`: number registered with signal-cli to send messages from. In the format `+12128675309`
- `SIGNAL_GROUP_ID`: base64 identifier for the Signal group to message, available with the `signal-cli listGroups` command. The sender should be a member of this group.

By default, we remove the Markdown formatting and links for better presentation in Signal. If you'd like to preserve those, you can pass the `--preserve-markdown` flag as an argument.

At runtime, the program generates a secret hex path that must be included with webhook requests. The full URL, including that path, is printed to stdout and to your logs, and takes the form of

```
https://puzzmo-webhooks.capybara-pangolin.ts.net/c41d9e80f14779e874bedaa6dfb8ac305cac3f2c7df3668e47a1fe39f829d9e8
```

(That path is saved in a config file that is placed in your `os.UserConfigDir()` directory, so it can persist across sessions. Delete that config file for a new path.)

You can specify that full URL to receive Puzzmo group messages by clicking the `Edit` button on the page for any group you administer, and set it as a `Discord Integration Webhook URL`.
