import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import proof from './proof'
import home from './home'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  admin,
  ...misc,
  ...proof,
  ...home,
}
