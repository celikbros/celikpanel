guest clock minus host clock (driver, measured once after the window): +0.132 s; guest instants below are corrected by it

| UTC (host) | source | what |
| --- | --- | --- |
| 13:07:41.713 | guest probe | answers (401) |
| 13:08:44.295 | network | navigation to / |
| 13:08:44.460 | network | navigation to / |
| 13:08:44.472 | network | GET /api/v1/panel/access-address FAILED net::ERR_ABORTED |
| 13:08:45.838 | network | GET /api/v1/license/access 200 |
| 13:08:45.872 | network | navigation to /setup |
| 13:08:48.338 | network | navigation to /settings?section=updates |
| 13:08:48.381 | network | navigation to /settings?section=updates |
| 13:08:48.400 | network | GET /api/v1/license/access 200 |
| 13:08:48.403 | network | GET /api/v1/panel/access-address FAILED net::ERR_ABORTED |
| 13:08:50.887 | browser | check-clicked |
| 13:08:52.046 | browser | offered |
| 13:08:52.052 | host probe | answers 401 |
| 13:08:52.055 | browser | start-clicked |
| 13:08:52.094 | screen | same document; Settings page mounted, section settings-updates-tab; no hold layer; page not inert; update window: Sending the update request…; /settings?section=updates [shots/update-000-130852.png] |
| 13:08:52.146 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:08:52.157 | guest journal | Starting celikpanel-self-update-<request>.service - [systemd-run] /opt/celikpanel/bin/agent --self-update-worker <request>... |
| 13:08:53.673 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:08:54.097 | screen | update window: The update is being applied; the panel may be unavailable briefly. [shots/update-001-130854.jpg] |
| 13:08:55.197 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:08:57.669 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:01.636 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:07.837 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:14.073 | guest journal | celikpanel-panel.service: Sent signal SIGSTOP to main process 5680 (panel) on client request. |
| 13:09:14.221 | guest journal | celikpanel-agent.service: Sent signal SIGSTOP to main process 5488 (agent) on client request. |
| 13:09:14.328 | guest probe | no answer (noanswer:RemoteDisconnected) |
| 13:09:14.851 | guest journal | Stopping celikpanel-panel.service - CelikPanel web panel... |
| 13:09:14.886 | guest journal | Stopped celikpanel-panel.service - CelikPanel web panel. |
| 13:09:14.992 | guest journal | Stopping celikpanel-agent.service - CelikPanel privileged agent... |
| 13:09:14.998 | guest journal | Stopped celikpanel-agent.service - CelikPanel privileged agent. |
| 13:09:15.239 | host probe | no answer noanswer:ECONNRESET |
| 13:09:17.704 | network | GET /api/v1/panel/update/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:18.010 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:18.156 | screen | update window: The connection was interrupted; the panel may be restarting. The same operation will be tr [shots/update-013-130918.jpg] |
| 13:09:32.740 | network | GET /api/v1/panel/update/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:33.015 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:33.028 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:34.219 | screen | hold layer over the page: Panel access could not be confirmed just now; page inert (data-access-hold=blocked); no full update window [shots/update-021-130934.png] |
| 13:09:38.016 | network | GET /api/v1/license/access FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:38.032 | network | GET /api/v1/recovery/status?request_id=... FAILED net::ERR_CONNECTION_CLOSED |
| 13:09:38.098 | guest journal | Starting celikpanel-agent.service - CelikPanel privileged agent... |
| 13:09:38.163 | guest journal | Started celikpanel-agent.service - CelikPanel privileged agent. |
| 13:09:38.891 | guest journal | Starting celikpanel-panel.service - CelikPanel web panel... |
| 13:09:38.964 | guest journal | Started celikpanel-panel.service - CelikPanel web panel. |
| 13:09:39.182 | guest journal | 2026/10/10 13:09:39 Panel startup listener active on :2083 (HTTPS; application gated) |
| 13:09:39.381 | guest probe | answers (401) |
| 13:09:39.486 | host probe | answers 401 |
| 13:09:43.028 | network | GET /api/v1/license/access 200 |
| 13:09:43.028 | network | GET /api/v1/recovery/status?request_id=... 200 |
| 13:09:43.072 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:44.264 | screen | no hold layer; page not inert [shots/update-026-130944.jpg] |
| 13:09:44.600 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:47.208 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:48.409 | network | GET /api/v1/license/access 200 |
| 13:09:49.490 | guest journal | celikpanel-self-update-<request>.service: Deactivated successfully. |
| 13:09:51.739 | network | GET /api/v1/panel/update/status?request_id=... 200 |
| 13:09:53.268 | network | navigation to /settings?section=updates&_cp_update=... |
| 13:09:53.299 | network | navigation to /settings?section=updates&_cp_update=... |
| 13:09:53.312 | network | navigation to /settings?section=updates |
| 13:09:53.322 | network | GET /api/v1/license/access 200 |
| 13:09:53.324 | network | GET /api/v1/panel/access-address FAILED net::ERR_ABORTED |
| 13:09:54.316 | screen | NEW document (marker gone: reload or replacement) [shots/update-031-130954.jpg] |
| 13:10:24.019 | network | GET /api/v1/license/access 200 |
| 13:10:53.332 | network | GET /api/v1/license/access 200 |
| 13:10:56.659 | browser | steady |
