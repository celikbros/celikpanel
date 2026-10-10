| route | profile | run | 0-200 doc ms | at 1 s | at 2 s | 'Checking panel access' interstitial | app from (doc ms) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `/setup` | plain | set7 (3 loads) | empty > page-loading > recovery-access(Checking panel access) | app | app | yes 135-231 / yes 124-207 / yes 119-199 | 247-282 |
| `/setup` | plain | set9 (3 loads) | empty > spinner > quiet / empty > spinner | app | app | no | 292-478 |
| `/setup` | throttled | set7 (3 loads) | nothing painted | empty | page-loading | yes 2258-3953 / yes 2263-3945 / yes 2249-3939 | 4654-4692 |
| `/setup` | throttled | set9 (3 loads) | nothing painted | empty | spinner | no | 4574-4630 |
| `/` | plain | set7 (3 loads) | empty > page-loading > recovery-access(Checking panel access) | app | app | yes 114-198 / yes 127-205 / yes 120-203 | 250-254 |
| `/` | plain | set9 (3 loads) | empty > spinner > quiet | app | app | no | 299-367 |
| `/` | throttled | set7 (3 loads) | nothing painted | empty | page-loading | yes 2253-3921 / yes 2247-3941 / yes 2243-3934 | 4654-4681 |
| `/` | throttled | set9 (3 loads) | nothing painted | empty | spinner | no | 4551-4585 |
| `/settings?section=updates` | plain | set7 (3 loads) | empty > page-loading > recovery-access(Checking panel access) | app | app | yes 118-203 / yes 143-223 / yes 150-230 | 316-337 |
| `/settings?section=updates` | plain | set9 (3 loads) | empty > spinner > quiet | app | app | no | 465-583 |
| `/settings?section=updates` | throttled | set7 (3 loads) | nothing painted | empty | page-loading | yes 2284-3986 / yes 2250-3931 / yes 2252-3953 | 4813-4862 |
| `/settings?section=updates` | throttled | set9 (3 loads) | nothing painted | empty | spinner | no | 4654-4860 |

plain-setup-1: t0 22 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 195 goto-ms; rule-breaking runs 0
   63-73:empty > 81-99:spinner > 118-300:quiet > 307-345:spinner > 478-521:app+worded > 551-602:app+worded > 636-6998:app+worded

plain-setup-2: t0 19 ms after timeOrigin; timeOrigin 10 ms after goto; first read answered 154 goto-ms; rule-breaking runs 0
   12-43:empty > 51-72:spinner > 87-210:quiet > 218-223:spinner > 292-332:app+worded > 359-362:app+worded > 399-6992:app+worded

plain-setup-3: t0 34 ms after timeOrigin; timeOrigin 40 ms after goto; first read answered 319 goto-ms; rule-breaking runs 0
   92-109:empty > 148-162:spinner > 212-374:quiet > 382-394:spinner > 473-509:app+worded > 545-545:app+worded > 563-6974:app+worded

plain-root-1: t0 37 ms after timeOrigin; timeOrigin 16 ms after goto; first read answered 260 goto-ms; rule-breaking runs 0
   32-105:empty > 113-151:spinner > 162-287:quiet > 294-306:spinner > 367-373:app+worded > 382-396:background > 427-429:app+worded > 458-6965:app+worded

plain-root-2: t0 22 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 218 goto-ms; rule-breaking runs 0
   70-82:empty > 91-114:spinner > 131-272:quiet > 278-289:spinner > 353-357:app+worded > 363-387:background > 425-428:app+worded > 444-6981:app+worded

plain-root-3: t0 19 ms after timeOrigin; timeOrigin 9 ms after goto; first read answered 150 goto-ms; rule-breaking runs 0
   10-43:empty > 51-83:spinner > 92-205:quiet > 214-226:spinner > 299-303:app+worded > 313-332:background > 366-369:app+worded > 406-6993:app+worded

plain-settings_section_updates-1: t0 19 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 163 goto-ms; rule-breaking runs 0
   14-45:empty > 53-83:spinner > 95-231:quiet > 238-466:spinner > 583-598:app+worded > 632-6988:app+worded

plain-settings_section_updates-2: t0 24 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 226 goto-ms; rule-breaking runs 0
   48-89:empty > 97-133:spinner > 143-297:quiet > 303-413:spinner > 513-525:app+worded > 573-6994:app+worded

plain-settings_section_updates-3: t0 24 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 209 goto-ms; rule-breaking runs 0
   46-81:empty > 89-124:spinner > 134-275:quiet > 282-387:spinner > 465-479:app+worded > 511-6979:app+worded

throttled-setup-1: t0 347 ms after timeOrigin; timeOrigin 13 ms after goto; first read answered 2972 goto-ms; rule-breaking runs 0
   883-1656:empty > 1665-2226:spinner > 2238-4148:quiet > 4154-4513:spinner > 4574-4852:app+worded > 4869-5027:app+worded > 5034-5735:app-shell-page-loading > 5757-6063:app+worded > 6073-8002:app+worded

throttled-setup-2: t0 339 ms after timeOrigin; timeOrigin 15 ms after goto; first read answered 3008 goto-ms; rule-breaking runs 0
   886-1666:empty > 1673-2226:spinner > 2239-4190:quiet > 4197-4551:spinner > 4630-5014:app+worded > 5021-5670:app-shell-page-loading > 5690-5990:app+worded > 6005-8002:app+worded

throttled-setup-3: t0 342 ms after timeOrigin; timeOrigin 12 ms after goto; first read answered 2985 goto-ms; rule-breaking runs 0
   892-1680:empty > 1687-2241:spinner > 2252-4192:quiet > 4200-4554:spinner > 4617-5005:app+worded > 5012-5670:app-shell-page-loading > 5690-6005:app+worded > 6020-8005:app+worded

throttled-root-1: t0 344 ms after timeOrigin; timeOrigin 14 ms after goto; first read answered 3002 goto-ms; rule-breaking runs 0
   797-1685:empty > 1692-2240:spinner > 2250-4154:quiet > 4161-4526:spinner > 4585-4859:app+worded > 4869-4985:background > 4991-5647:spinner > 5669-5973:app+worded > 5987-8003:app+worded

throttled-root-2: t0 339 ms after timeOrigin; timeOrigin 14 ms after goto; first read answered 2965 goto-ms; rule-breaking runs 0
   901-1681:empty > 1688-2244:spinner > 2256-4141:quiet > 4147-4501:spinner > 4557-4826:app+worded > 4836-4953:background > 4959-5608:spinner > 5631-5941:app+worded > 5958-8001:app+worded

throttled-root-3: t0 339 ms after timeOrigin; timeOrigin 12 ms after goto; first read answered 2962 goto-ms; rule-breaking runs 0
   890-1671:empty > 1678-2233:spinner > 2242-4148:quiet > 4155-4492:spinner > 4551-4822:app+worded > 4833-4954:background > 4961-5602:spinner > 5624-5924:app+worded > 5938-8003:app+worded

throttled-settings_section_updates-1: t0 335 ms after timeOrigin; timeOrigin 11 ms after goto; first read answered 2990 goto-ms; rule-breaking runs 0
   911-1699:empty > 1706-2258:spinner > 2269-4300:quiet > 4307-4803:spinner > 4860-5131:app-shell-page-loading+worded > 5138-6125:app-shell-page-loading+worded > 6140-8003:app+worded

throttled-settings_section_updates-2: t0 333 ms after timeOrigin; timeOrigin 13 ms after goto; first read answered 2938 goto-ms; rule-breaking runs 0
   895-1661:empty > 1667-2210:spinner > 2220-4131:quiet > 4137-4640:spinner > 4689-4961:app-shell-page-loading+worded > 4969-5915:app-shell-page-loading+worded > 5930-8004:app+worded

throttled-settings_section_updates-3: t0 329 ms after timeOrigin; timeOrigin 14 ms after goto; first read answered 2924 goto-ms; rule-breaking runs 0
   897-1649:empty > 1661-2212:spinner > 2220-4096:quiet > 4103-4600:spinner > 4654-4928:app-shell-page-loading+worded > 4945-5894:app-shell-page-loading+worded > 5908-8006:app+worded
