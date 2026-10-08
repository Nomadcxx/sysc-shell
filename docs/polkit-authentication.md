# PolicyKit authentication

sysc-shell can show the authentication prompt used by PolicyKit applications such as `pkexec`.
The prompt uses the system PolicyKit helper and PAM; terminal `sudo` prompts stay in the terminal.

Open Settings → Session and choose **Authentication agent**:

- **auto** registers sysc-shell when the Niri session has no other authentication agent. This is the default.
- **on** retries registration every 30 seconds while another agent holds the session.
- **off** disables registration.

The status card reports whether sysc-shell registered, stood down for another agent, or could not
connect to PolicyKit. The prompt waits while sysc-shell's configured session locker is active.

You can also set the policy in `$XDG_CONFIG_HOME/sysc-shell/config.json`:

```json
{
  "session": {
    "polkit_agent": "auto"
  }
}
```
