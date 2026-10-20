# API 接口清单

> 自动生成，请勿手工编辑。重新生成：`python3 docs/tools/gen-api-inventory.py`。

- 来源：`routers/router.go` + `shop/router.go`
- 生成时间：2026-10-18 19:10
- 接口总数：**245**（去重后 `METHOD + 路径`，路径参数统一记为 `:param`）

## 汇总

| 分类 | 接口数 | GET | POST | PUT | PATCH | DELETE | HEAD | OPTIONS |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 公开 | 22 | 8 | 10 | 1 | 0 | 3 | 0 | 0 |
| 用户（登录） | 89 | 32 | 26 | 11 | 6 | 11 | 2 | 1 |
| 小店 | 45 | 24 | 17 | 0 | 4 | 0 | 0 | 0 |
| 管理员 | 78 | 22 | 39 | 11 | 1 | 5 | 0 | 0 |
| 从机 / 回调 | 11 | 3 | 5 | 2 | 0 | 1 | 0 | 0 |
| **合计** | **245** | 89 | 97 | 25 | 11 | 20 | 2 | 1 |

## 公开（22）

| 方法 | 路径 | 处理器 |
| --- | --- | --- |
| `POST` | `/api/v4/s/report` | `SubmitAbuseReport` |
| `POST` | `/api/v4/session/authn` | `FinishLoginAuthn` |
| `PUT` | `/api/v4/session/authn` | `StartLoginAuthn` |
| `GET` | `/api/v4/session/oauth/app/:app_id` | `GetAppRegistration` |
| `POST` | `/api/v4/session/oauth/consent` | `GrantAppConsent` |
| `POST` | `/api/v4/session/oauth/consent/deny` | `DenyAppConsent` |
| `DELETE` | `/api/v4/session/oauth/grant/:app_id` | `DeleteOAuthGrant` |
| `POST` | `/api/v4/session/oauth/token` | `FromForm` |
| `GET` | `/api/v4/session/oauth/userinfo` | `OpenIDUserInfo` |
| `GET` | `/api/v4/session/prepare` | `UserPrepareLogin` |
| `DELETE` | `/api/v4/session/token` | `UserSignOut` |
| `POST` | `/api/v4/session/token` | `UserLoginValidation` |
| `POST` | `/api/v4/session/token/2fa` | `UserLogin2FAValidation` |
| `POST` | `/api/v4/session/token/refresh` | `UserRefreshToken` |
| `DELETE` | `/api/v4/share/:id` | `DeleteShare` |
| `POST` | `/api/v4/share/:id` | `EditShare` |
| `POST` | `/api/v4/site/abuse` | `SubmitAbuseReport` |
| `GET` | `/api/v4/site/captcha` | `Captcha` |
| `GET` | `/api/v4/site/config/:section` | `SiteConfig` |
| `GET` | `/api/v4/site/ping` | `Ping` |
| `GET` | `/api/v4/vas/payment/status` | `VASPaymentStatus` |
| `GET` | `/api/v4/vas/payment/status/:order/:channel` | `VASPaymentStatus` |

## 用户（登录）（89）

| 方法 | 路径 | 处理器 |
| --- | --- | --- |
| `GET` | `/api/v4/devices/dav` | `ListDavAccounts` |
| `PUT` | `/api/v4/devices/dav` | `CreateDAVAccounts` |
| `DELETE` | `/api/v4/devices/dav/:id` | `DeleteDAVAccounts` |
| `PATCH` | `/api/v4/devices/dav/:id` | `UpdateDAVAccounts` |
| `POST` | `/api/v4/download/cancel` | `SlaveCancelDownloadTask` |
| `POST` | `/api/v4/download/select` | `SlaveSelectFilesToDownload` |
| `POST` | `/api/v4/download/status` | `SlaveDownloadTaskStatus` |
| `POST` | `/api/v4/download/task` | `SlaveDownloadTaskCreate` |
| `POST` | `/api/v4/download/test` | `SlaveTestDownloader` |
| `DELETE` | `/api/v4/file` | `SlaveDelete` |
| `GET` | `/api/v4/file` | `ListDirectory` |
| `GET` | `/api/v4/file/archive` | `ListArchiveFiles` |
| `GET` | `/api/v4/file/archive/:sessionID/archive.zip` | `DownloadArchive` |
| `PUT` | `/api/v4/file/content` | `PutContent` |
| `OPTIONS` | `/api/v4/file/content/*option` | `ServeEntity` |
| `GET` | `/api/v4/file/content/:id/:speed/:name` | `ServeEntity` |
| `HEAD` | `/api/v4/file/content/:id/:speed/:name` | `ServeEntity` |
| `GET` | `/api/v4/file/content/:nodeId/:src/:speed/:name` | `SlaveServeEntity` |
| `HEAD` | `/api/v4/file/content/:nodeId/:src/:speed/:name` | `SlaveServeEntity` |
| `POST` | `/api/v4/file/create` | `CreateFile` |
| `GET` | `/api/v4/file/events` | `HandleExplorerEventsPush` |
| `GET` | `/api/v4/file/info` | `GetFileInfo` |
| `GET` | `/api/v4/file/list` | `SlaveList` |
| `DELETE` | `/api/v4/file/lock` | `Unlock` |
| `GET` | `/api/v4/file/meta/:src/:ext` | `SlaveMeta` |
| `PATCH` | `/api/v4/file/metadata` | `PatchMetadata` |
| `POST` | `/api/v4/file/move` | `MoveFile` |
| `DELETE` | `/api/v4/file/pin` | `Unpin` |
| `PUT` | `/api/v4/file/pin` | `Pin` |
| `POST` | `/api/v4/file/rename` | `RenameFile` |
| `POST` | `/api/v4/file/restore` | `Restore` |
| `GET` | `/api/v4/file/search` | `FulltextSearch` |
| `PUT` | `/api/v4/file/source` | `GetSource` |
| `DELETE` | `/api/v4/file/source/:id` | `DeleteDirectLink` |
| `GET` | `/api/v4/file/thumb` | `Thumb` |
| `GET` | `/api/v4/file/thumb/:src/:ext` | `SlaveThumb` |
| `DELETE` | `/api/v4/file/trash` | `EmptyTrash` |
| `DELETE` | `/api/v4/file/upload` | `DeleteUploadSession` |
| `PUT` | `/api/v4/file/upload` | `CreateUploadSession` |
| `POST` | `/api/v4/file/upload/:sessionId/:index` | `FileUpload` |
| `POST` | `/api/v4/file/url` | `FileURL` |
| `DELETE` | `/api/v4/file/version` | `DeleteVersion` |
| `POST` | `/api/v4/file/version/current` | `SetCurrentVersion` |
| `PATCH` | `/api/v4/file/view` | `PatchView` |
| `PUT` | `/api/v4/file/viewerSession` | `CreateViewerSession` |
| `POST` | `/api/v4/ping` | `SlavePing` |
| `DELETE` | `/api/v4/share` | `BatchDeleteShare` |
| `GET` | `/api/v4/share` | `ListShare` |
| `PUT` | `/api/v4/share` | `CreateShare` |
| `GET` | `/api/v4/share/default` | `ListDefaultShare` |
| `GET` | `/api/v4/share/discover` | `DiscoverShare` |
| `GET` | `/api/v4/share/info/:id` | `GetShare` |
| `POST` | `/api/v4/share/redeem` | `RedeemShare` |
| `POST` | `/api/v4/share/report` | `SubmitAbuseReport` |
| `PUT` | `/api/v4/task` | `SlaveCreateTask` |
| `GET` | `/api/v4/task/:id` | `SlaveGetTask` |
| `POST` | `/api/v4/task/cleanup` | `SlaveCleanupFolder` |
| `POST` | `/api/v4/user` | `-` |
| `GET` | `/api/v4/user/activate/:id` | `UserActivate` |
| `DELETE` | `/api/v4/user/authn` | `UserDeletePasskey` |
| `POST` | `/api/v4/user/authn` | `FinishRegAuthn` |
| `PUT` | `/api/v4/user/authn` | `StartRegAuthn` |
| `GET` | `/api/v4/user/avatar/:id` | `GetUserAvatar` |
| `GET` | `/api/v4/user/capacity` | `UserStorage` |
| `GET` | `/api/v4/user/info/:id` | `UserGet` |
| `GET` | `/api/v4/user/me` | `UserMe` |
| `GET` | `/api/v4/user/payments` | `VASListMyPayments` |
| `POST` | `/api/v4/user/reset` | `UserSendReset` |
| `PATCH` | `/api/v4/user/reset/:id` | `UserReset` |
| `GET` | `/api/v4/user/search` | `UserSearch` |
| `GET` | `/api/v4/user/setting` | `UserSetting` |
| `PATCH` | `/api/v4/user/setting` | `UpdateOption` |
| `GET` | `/api/v4/user/setting/2fa` | `UserInit2FA` |
| `PUT` | `/api/v4/user/setting/avatar` | `UploadAvatar` |
| `GET` | `/api/v4/user/shares/:id` | `ListPublicShare` |
| `POST` | `/api/v4/vas/giftcode/redeem` | `VASRedeemGiftCode` |
| `PUT` | `/api/v4/vas/payment` | `VASCreatePayment` |
| `GET` | `/api/v4/vas/points` | `VASGetPoints` |
| `GET` | `/api/v4/vas/skus` | `VASListSKUs` |
| `GET` | `/api/v4/workflow` | `ListTasks` |
| `POST` | `/api/v4/workflow/archive` | `CreateArchive` |
| `POST` | `/api/v4/workflow/download` | `CreateRemoteDownload` |
| `DELETE` | `/api/v4/workflow/download/:id` | `CancelDownloadTask` |
| `PATCH` | `/api/v4/workflow/download/:id` | `SetDownloadTaskTarget` |
| `POST` | `/api/v4/workflow/extract` | `ExtractArchive` |
| `POST` | `/api/v4/workflow/import` | `ImportFiles` |
| `GET` | `/api/v4/workflow/progress/:id` | `GetTaskPhaseProgress` |
| `POST` | `/api/v4/workflow/rebuildFtsIndex` | `RebuildFTSIndex` |
| `POST` | `/api/v4/workflow/relocate` | `RelocateFiles` |

## 小店（45）

| 方法 | 路径 | 处理器 |
| --- | --- | --- |
| `PATCH` | `/api/v4/shop/shop` | `-` |
| `POST` | `/api/v4/shop/shop` | `-` |
| `POST` | `/api/v4/shop/shop/admin/order/:orderNo/refund` | `-` |
| `POST` | `/api/v4/shop/shop/admin/reconcile` | `-` |
| `GET` | `/api/v4/shop/shop/admin/refund` | `-` |
| `POST` | `/api/v4/shop/shop/admin/refund/:id` | `-` |
| `GET` | `/api/v4/shop/shop/admin/setting` | `-` |
| `PATCH` | `/api/v4/shop/shop/admin/setting` | `-` |
| `GET` | `/api/v4/shop/shop/admin/shop` | `-` |
| `GET` | `/api/v4/shop/shop/admin/shop/:id/stats` | `-` |
| `GET` | `/api/v4/shop/shop/admin/withdraw` | `-` |
| `POST` | `/api/v4/shop/shop/admin/withdraw/:id` | `-` |
| `GET` | `/api/v4/shop/shop/asset` | `-` |
| `POST` | `/api/v4/shop/shop/asset` | `-` |
| `GET` | `/api/v4/shop/shop/asset/:id` | `-` |
| `POST` | `/api/v4/shop/shop/asset/adopt` | `-` |
| `GET` | `/api/v4/shop/shop/balance` | `-` |
| `GET` | `/api/v4/shop/shop/coupon` | `-` |
| `POST` | `/api/v4/shop/shop/coupon` | `-` |
| `PATCH` | `/api/v4/shop/shop/coupon/:id` | `-` |
| `GET` | `/api/v4/shop/shop/epay/notify` | `-` |
| `POST` | `/api/v4/shop/shop/epay/notify` | `-` |
| `GET` | `/api/v4/shop/shop/mine` | `-` |
| `GET` | `/api/v4/shop/shop/order` | `-` |
| `POST` | `/api/v4/shop/shop/order` | `-` |
| `GET` | `/api/v4/shop/shop/order/:orderNo` | `-` |
| `GET` | `/api/v4/shop/shop/product` | `-` |
| `POST` | `/api/v4/shop/shop/product` | `-` |
| `PATCH` | `/api/v4/shop/shop/product/:id` | `-` |
| `POST` | `/api/v4/shop/shop/product/:id/status` | `-` |
| `GET` | `/api/v4/shop/shop/purchase` | `-` |
| `POST` | `/api/v4/shop/shop/purchase/:id/refund` | `-` |
| `POST` | `/api/v4/shop/shop/purchase/:id/transfer` | `-` |
| `GET` | `/api/v4/shop/shop/referral` | `-` |
| `GET` | `/api/v4/shop/shop/referral/link` | `-` |
| `GET` | `/api/v4/shop/shop/refund` | `-` |
| `POST` | `/api/v4/shop/shop/refund/:id` | `-` |
| `GET` | `/api/v4/shop/shop/refund/received` | `-` |
| `GET` | `/api/v4/shop/shop/sold` | `-` |
| `POST` | `/api/v4/shop/shop/status` | `-` |
| `GET` | `/api/v4/shop/shop/store/:slug` | `-` |
| `GET` | `/api/v4/shop/shop/store/:slug/product/:id` | `-` |
| `GET` | `/api/v4/shop/shop/store/:slug/product/:id/preview` | `-` |
| `GET` | `/api/v4/shop/shop/withdraw` | `-` |
| `POST` | `/api/v4/shop/shop/withdraw` | `-` |

## 管理员（78）

| 方法 | 路径 | 处理器 |
| --- | --- | --- |
| `POST` | `/api/v4/admin/abuse` | `AdminAbuseList` |
| `POST` | `/api/v4/admin/abuse/:id` | `AdminAbuseHandle` |
| `POST` | `/api/v4/admin/abuse/batch/delete` | `AdminAbuseDelete` |
| `POST` | `/api/v4/admin/audit` | `AdminAuditList` |
| `POST` | `/api/v4/admin/entity` | `AdminListEntities` |
| `GET` | `/api/v4/admin/entity/:id` | `AdminGetEntity` |
| `POST` | `/api/v4/admin/entity/batch/delete` | `AdminBatchDeleteEntity` |
| `GET` | `/api/v4/admin/entity/url/:id` | `AdminGetEntityUrl` |
| `POST` | `/api/v4/admin/event` | `AdminAuditList` |
| `GET` | `/api/v4/admin/event/:id` | `AdminGetAudit` |
| `POST` | `/api/v4/admin/event/batch/delete` | `AdminAuditBatchDelete` |
| `POST` | `/api/v4/admin/event/cleanup` | `AdminAuditCleanup` |
| `POST` | `/api/v4/admin/file` | `AdminListFiles` |
| `GET` | `/api/v4/admin/file/:id` | `AdminGetFile` |
| `PUT` | `/api/v4/admin/file/:id` | `AdminUpdateFile` |
| `POST` | `/api/v4/admin/file/batch/delete` | `AdminBatchDeleteFile` |
| `GET` | `/api/v4/admin/file/url/:id` | `AdminGetFileUrl` |
| `GET` | `/api/v4/admin/file_stats` | `AdminFileStats` |
| `POST` | `/api/v4/admin/group` | `AdminListGroups` |
| `PUT` | `/api/v4/admin/group` | `AdminCreateGroup` |
| `DELETE` | `/api/v4/admin/group/:id` | `AdminDeleteGroup` |
| `GET` | `/api/v4/admin/group/:id` | `AdminGetGroup` |
| `PUT` | `/api/v4/admin/group/:id` | `AdminUpdateGroup` |
| `POST` | `/api/v4/admin/node` | `AdminListNodes` |
| `PUT` | `/api/v4/admin/node` | `AdminCreateNode` |
| `DELETE` | `/api/v4/admin/node/:id` | `AdminDeleteNode` |
| `GET` | `/api/v4/admin/node/:id` | `AdminGetNode` |
| `PUT` | `/api/v4/admin/node/:id` | `AdminUpdateNode` |
| `POST` | `/api/v4/admin/node/test` | `AdminTestSlave` |
| `POST` | `/api/v4/admin/node/test/downloader` | `AdminTestDownloader` |
| `POST` | `/api/v4/admin/oauthClient` | `AdminListOAuthClients` |
| `PUT` | `/api/v4/admin/oauthClient` | `AdminCreateOAuthClient` |
| `DELETE` | `/api/v4/admin/oauthClient/:id` | `AdminDeleteOAuthClient` |
| `GET` | `/api/v4/admin/oauthClient/:id` | `AdminGetOAuthClient` |
| `PUT` | `/api/v4/admin/oauthClient/:id` | `AdminUpdateOAuthClient` |
| `POST` | `/api/v4/admin/oauthClient/batch/delete` | `AdminBatchDeleteOAuthClient` |
| `POST` | `/api/v4/admin/payment` | `VASAdminListPayments` |
| `POST` | `/api/v4/admin/payment/batch/delete` | `VASAdminDeletePayments` |
| `POST` | `/api/v4/admin/policy` | `AdminListPolicies` |
| `PUT` | `/api/v4/admin/policy` | `AdminCreatePolicy` |
| `DELETE` | `/api/v4/admin/policy/:id` | `AdminDeletePolicy` |
| `GET` | `/api/v4/admin/policy/:id` | `AdminGetPolicy` |
| `PUT` | `/api/v4/admin/policy/:id` | `AdminUpdatePolicy` |
| `POST` | `/api/v4/admin/policy/cors` | `AdminCreateStoragePolicyCors` |
| `POST` | `/api/v4/admin/policy/oauth/callback` | `AdminFinishOauthCallback` |
| `GET` | `/api/v4/admin/policy/oauth/redirect` | `AdminGetPolicyOAuthCallbackURL` |
| `GET` | `/api/v4/admin/policy/oauth/root/:id` | `AdminGetSharePointDriverRoot` |
| `POST` | `/api/v4/admin/policy/oauth/signin` | `AdminOdOAuthURL` |
| `GET` | `/api/v4/admin/policy/oauth/status/:id` | `AdminGetPolicyOAuthStatus` |
| `POST` | `/api/v4/admin/queue` | `AdminListTasks` |
| `GET` | `/api/v4/admin/queue/:id` | `AdminGetTask` |
| `POST` | `/api/v4/admin/queue/batch/delete` | `AdminBatchDeleteTask` |
| `POST` | `/api/v4/admin/queue/cleanup` | `AdminCleanupTask` |
| `GET` | `/api/v4/admin/queue/metrics` | `AdminGetQueueMetrics` |
| `PATCH` | `/api/v4/admin/settings` | `AdminSetSettings` |
| `POST` | `/api/v4/admin/settings` | `AdminGetSettings` |
| `POST` | `/api/v4/admin/share` | `AdminListShares` |
| `GET` | `/api/v4/admin/share/:id` | `AdminGetShare` |
| `POST` | `/api/v4/admin/share/batch/delete` | `AdminBatchDeleteShare` |
| `GET` | `/api/v4/admin/summary` | `AdminSummary` |
| `DELETE` | `/api/v4/admin/tool/entityUrlCache` | `AdminClearEntityUrlCache` |
| `POST` | `/api/v4/admin/tool/mail` | `AdminSendTestMail` |
| `POST` | `/api/v4/admin/tool/thumbExecutable` | `AdminTestThumbGenerator` |
| `GET` | `/api/v4/admin/tool/wopi` | `AdminFetchWopi` |
| `POST` | `/api/v4/admin/user` | `AdminListUsers` |
| `PUT` | `/api/v4/admin/user` | `AdminCreateUser` |
| `GET` | `/api/v4/admin/user/:id` | `AdminGetUser` |
| `PUT` | `/api/v4/admin/user/:id` | `AdminUpdateUser` |
| `POST` | `/api/v4/admin/user/:id/calibrate` | `AdminCalibrateStorage` |
| `POST` | `/api/v4/admin/user/:id/points` | `AdminUserAdjustPoints` |
| `POST` | `/api/v4/admin/user/batch/delete` | `AdminDeleteUser` |
| `GET` | `/api/v4/admin/vas/giftcode` | `VASAdminListGiftCodes` |
| `POST` | `/api/v4/admin/vas/giftcode` | `VASAdminCreateGiftCodes` |
| `POST` | `/api/v4/admin/vas/giftcode/batch/delete` | `VASAdminDeleteGiftCodes` |
| `GET` | `/api/v4/admin/vas/providers` | `VASListProviders` |
| `POST` | `/api/v4/admin/vas/providers` | `VASSaveProviders` |
| `GET` | `/api/v4/admin/vas/skus` | `VASListSKUs` |
| `POST` | `/api/v4/admin/vas/skus` | `VASSaveSKUs` |

## 从机 / 回调（11）

| 方法 | 路径 | 处理器 |
| --- | --- | --- |
| `GET` | `/api/v4/payment/notify/custom/:pid` | `VASCustomNotify` |
| `GET` | `/api/v4/payment/notify/epay/:pid` | `VASEpayNotify` |
| `POST` | `/api/v4/payment/notify/epay/:pid` | `VASEpayNotify` |
| `GET` | `/api/v4/slave/credential/:id` | `SlaveGetCredential` |
| `POST` | `/api/v4/slave/statelessUpload/complete` | `StatelessCompleteUpload` |
| `POST` | `/api/v4/slave/statelessUpload/create` | `StatelessCreateFile` |
| `POST` | `/api/v4/slave/statelessUpload/failed` | `StatelessOnUploadFailed` |
| `PUT` | `/api/v4/slave/statelessUpload/prepare` | `StatelessPrepareUpload` |
| `PUT` | `/api/v4/upload` | `SlaveGetUploadSession` |
| `DELETE` | `/api/v4/upload/:sessionId` | `SlaveDeleteUploadSession` |
| `POST` | `/api/v4/upload/:sessionId` | `SlaveUpload` |

