import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import proof from './proof'
import home from './home'
// APEXONE-EXT: 双边市场文案（纯新增模块，未改动上游任何一个命名空间）
import supply from './supply'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...proof,
  ...home,
  ...supply,
}
