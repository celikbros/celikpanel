guest clock minus host clock (driver, measured once after the window): +0.202 s; guest instants below are corrected by it

| UTC (host) | source | what |
| --- | --- | --- |
| 13:45:56.904 | guest probe | answers (401) |
| 13:46:19.532 | network | navigation to / |
| 13:46:19.865 | network | navigation to / |
| 13:46:21.336 | network | GET /api/v1/license/access 200 |
| 13:46:21.390 | network | navigation to /setup |
| 13:46:23.996 | network | navigation to /settings?section=updates |
| 13:46:24.062 | network | navigation to /settings?section=updates |
| 13:46:24.090 | network | GET /api/v1/license/access 200 |
| 13:46:24.092 | network | GET /api/v1/panel/access-address FAILED net::ERR_ABORTED |
| 13:46:26.655 | browser | check-clicked |
| 13:46:27.808 | browser | offered |
| 13:46:27.815 | host probe | answers 401 |
| 13:46:27.818 | browser | start-clicked |
| 13:46:27.871 | screen | same document; Settings page mounted, section settings-updates-tab; no hold layer; page not inert; no full update window; /settings?section=updates [shots/update-000-134627.png] |
| 13:46:27.906 | guest journal | Starting celikpanel-self-update-<request>.service - [systemd-run] /opt/celikpanel/bin/agent --self-update-worker <request>... |
| 13:46:27.936 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:29.492 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:29.877 | screen | update window: The update is being applied; the panel may be unavailable briefly. [shots/update-001-134629.jpg] |
| 13:46:31.091 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:34.589 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:38.608 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:44.870 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:46:55.638 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:47:06.028 | network | GET /api/v1/license/access 200 |
| 13:47:10.747 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:47:24.092 | network | GET /api/v1/license/access 200 |
| 13:47:26.087 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:47:41.569 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:47:46.088 | guest journal | celikpanel-panel.service: Sent signal SIGSTOP to main process 5278 (panel) on client request. |
| 13:47:46.220 | guest probe | no answer (noanswer:URLError) |
| 13:47:46.328 | guest journal | celikpanel-agent.service: Sent signal SIGSTOP to main process 5090 (agent) on client request. |
| 13:47:46.506 | host probe | no answer noanswer:ECONNRESET |
| 13:47:48.361 | guest journal | Stopping celikpanel-panel.service - CelikPanel web panel... |
| 13:47:48.470 | guest journal | Stopped celikpanel-panel.service - CelikPanel web panel. |
| 13:47:48.781 | guest journal | Stopping celikpanel-agent.service - CelikPanel privileged agent... |
| 13:47:48.796 | guest journal | Stopped celikpanel-agent.service - CelikPanel privileged agent. |
| 13:47:51.154 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:47:56.654 | network | GET /api/v1/panel/update/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:47:58.300 | screen | update window: The connection was interrupted; the panel may be restarting. The same operation will be tr [shots/update-045-134758.jpg] |
| 13:48:06.032 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:06.063 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:06.333 | screen | full-screen replacement: License status could not be checked / Update and recovery status; no full update window [shots/update-049-134806.jpg] |
| 13:48:11.087 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:16.044 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:21.051 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:24.126 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:26.053 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:31.078 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:36.053 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:41.054 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:46.106 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:51.083 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:48:56.047 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:49:01.042 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:49:06.070 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:49:11.046 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:49:12.721 | guest journal | Starting celikpanel-agent.service - CelikPanel privileged agent... |
| 13:49:12.822 | guest journal | Started celikpanel-agent.service - CelikPanel privileged agent. |
| 13:49:14.036 | guest journal | Starting celikpanel-panel.service - CelikPanel web panel... |
| 13:49:14.136 | guest journal | Started celikpanel-panel.service - CelikPanel web panel. |
| 13:49:14.676 | guest journal | 2026/10/10 13:49:14 Panel startup listener active on :2083 (HTTPS; application gated) |
| 13:49:14.989 | guest probe | answers (401) |
| 13:49:15.409 | host probe | answers 401 |
| 13:49:16.041 | network | GET /api/v1/recovery/status?request_id=... 200 |
| 13:49:21.029 | network | GET /api/v1/recovery/status?request_id=... 200 |
| 13:49:24.086 | network | GET /api/v1/license/access 200 |
| 13:49:24.206 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:49:24.795 | screen | Settings page mounted, section settings-updates-tab [shots/update-088-134924.jpg] |
| 13:49:25.736 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:49:26.912 | guest journal | celikpanel-self-update-<request>.service: Deactivated successfully. |
| 13:49:28.888 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:49:30.430 | network | navigation to /settings?section=updates&_cp_update=... |
| 13:49:30.591 | network | navigation to /settings?section=updates&_cp_update=... |
| 13:49:30.624 | network | navigation to /settings?section=updates |
| 13:49:30.625 | network | GET /api/v1/license/access 200 |
| 13:49:30.820 | screen | NEW document (marker gone: reload or replacement) [shots/update-091-134930.jpg] |
| 13:50:00.022 | network | GET /api/v1/license/access 200 |
| 13:50:30.639 | network | GET /api/v1/license/access 200 |
| 13:50:31.214 | browser | steady |
