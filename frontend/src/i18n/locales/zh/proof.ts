// /proof 页文案(单 enclave 架构)。注意:vue-i18n 把 "@" 和 "|" 当特殊语法,
// 字面 @ 要写成 {'@'};digest、命令一律不进文案,由组件直接渲染。
export default {
  proof: {
    header: {
      backHome: '首页'
    },
    hero: {
      title: '跟着你的请求走——每一步都能亲自验证。',
      subtitle:
        '本页展示你的请求一路经过哪些环节、每一步我们能证明什么。整个服务运行在一台受硬件保护的机密电脑(Intel TDX 机密 VM)里,跑的是公开、可逐行审计的开源代码。',
      scopeNote:
        '每一项验证都在你自己的浏览器里、对着芯片厂商 Intel 的信任根完成,不经过我们的服务器。每一步只有在全部密码学校验通过时才会变绿。'
    },
    openSource: {
      title: '别信我们,去看代码',
      body: '这不是一个黑盒。这台机密电脑里跑的每一行代码都是开源的,并锁定到确切版本——任何人都能逐行审计源码、用下方公布的指纹亲手复核,验证它与本页实时证明完全一致。',
      serviceLabel: '服务源码',
      serviceSub: '后端 + 前端,机密电脑里跑的全部应用代码',
      deployLabel: '部署与参考值',
      deploySub: '被度量的 compose 文件与公布的指纹(reference.json)',
      ciLabel: '构建流水线',
      ciSub: '公开 CI:构建镜像、cosign 签名、SLSA 来源证明',
      viewSource: '查看源码 →',
      pinnedAt: "锁定 {'@'} {commit}"
    },
    status: {
      pending: '待验证',
      checking: '验证中…',
      pass: '已通过',
      fail: '未通过',
      disclosure: '服务商侧'
    },
    journey: {
      title: '你的请求经过的旅程',
      basisLabel: '凭据',
      hop1: {
        node: '你的设备 → publicai-gateway(运行在机密硬件里)',
        claim:
          '你的请求经加密连接送达一台受硬件保护的机密电脑(机密 VM);它跑的代码可以通过核对代码指纹(compose_hash)确认——指纹由 Intel 硬件度量并签名,伪造不了。',
        basis: '代码指纹核对 + Intel 硬件出具的证明(TDX)'
      },
      hop2: {
        node: 'publicai-gateway(机密硬件)',
        claim:
          '这台机密电脑跑的是公开、可审计的确切代码。证明是"现在"出具的:你的浏览器生成一个一次性随机数,硬件把它绑进证明里——拿旧报告糊弄不了你。',
        basis: '一次性随机数绑定 + 代码指纹 → 公开源码版本'
      },
      hop3: {
        node: 'publicai-gateway → 上游模型服务商',
        claim:
          '这一步无法保密:上游服务商(如 Anthropic、OpenAI)会按它们自己的政策解密并处理你的明文。我们只能证明"请求确实是从受认证的机密电脑发出的"。',
        basis: '诚实说明——这不是保密承诺'
      }
    },
    flow: {
      device: '你的设备',
      deviceSub: '发起请求',
      teeLabel: 'publicai-gateway · Intel TDX 机密 VM(硬件隔离、内存加密)',
      service: 'sub2api TEE',
      serviceSub: '请求在此处理',
      attestor: '证明服务',
      attestorSub: '出具实时硬件证明',
      upstream: '上游服务商',
      upstreamSub: '看到明文(按其政策)',
      arrEncrypted: '加密',
      arrInternal: '同一机密 VM',
      arrPlaintext: '明文',
      legendConfidential: '受保护:在机密电脑内部,运营方和中间环节都改不了它跑的代码',
      legendPlaintext: '明文:只有最后一步,由上游服务商按其隐私政策处理',
      caption:
        '一句话:整个服务跑在一台受硬件保护、代码可验证的机密电脑里——它跑的是哪份代码、是不是现在在跑,都能在你的浏览器里当场验证。'
    },
    disclosure: {
      title: '诚实说明:上游服务商会看到明文',
      body:
        '模型最终由上游服务商(如 Anthropic、OpenAI)提供,所以它们必然会按自己的政策处理转发过去的请求内容。另外,你与本站之间的加密连接(TLS)终止在机密电脑前面的 Phala 网关——本页未在其上叠加端到端加密。我们证明的是:这台机密电脑跑的确实是公开、未被篡改的代码。'
    },
    verify: {
      checksTitle: '逐项检查结果',
      running: {
        title: '正在你的浏览器里验证…',
        note: '正在读取硬件证明、核对随机数绑定,并用 Intel 的官方数据验证这份硬件证明是真的(首次会加载约 426KB 的验证程序)。'
      },
      pass: {
        title: '硬件级验证通过',
        note: '这是真正的 Intel 机密硬件,跑的正是公布指纹对应的确切代码,且证明绑定到了你这次的一次性随机数上——是刚生成的,不是回放的旧数据。诚实边界:最后一步上游服务商按设计会看到明文。'
      },
      fail: {
        title: '验证未通过',
        note: '有一项或多项关键检查没通过。请不要信任这个服务——见下方逐项结果。'
      },
      error: {
        title: '验证没能完成',
        note: '没能取到硬件证明或验证数据(网络/服务暂时不可达)。这不算失败也不算通过——请重试。'
      },
      lastVerified: '最近验证于 {time} UTC',
      rerun: '重新验证',
      checkLabel: {
        quote_present: '证明数据完整性',
        quote_genuine: '真正的 Intel 机密硬件',
        tcb_status: '硬件安全补丁级别',
        nonce_binding: '一次性随机数绑定',
        measurement_rtmr3_replay: '度量日志真实性(RTMR3 重放)',
        measurement_app_id: '应用身份 (app-id)',
        measurement_compose_hash_eventlog: '部署内容指纹(度量日志)',
        measurement_compose_hash_mrconfigid: '部署内容指纹(硬件寄存器)',
        measurement_os_image_hash: '操作系统镜像指纹',
        measurement_mrtd_reference: '底层固件寄存器(仅展示)'
      }
    },
    live: {
      title: '实时读取硬件证明',
      desc:
        '现在向机密电脑的证明服务索取一份最新硬件证明,并绑定一个刚生成的一次性随机数(防止别人拿旧报告糊弄你)。它的硬件度量值会和下方公布的参考值逐条对照。重点:只有当硬件证明经过密码学验证后,这份报告才可信——本页已经在你的浏览器里替你完成了这项验证。',
      fetch: '读取并验证证明',
      fetching: '读取并验证中…',
      download: '下载证据(evidence.json)',
      nonce: '一次性随机数',
      error: '无法访问硬件证明服务。',
      endpointLabel: '证明地址',
      collateralLabel: 'Intel 官方验证数据',
      explorerLabel: '外部验证工具',
      referenceLabel: '公布的参考值',
      refetch: '重新验证',
      autoNote: '💡 打开本页时会自动在你的浏览器里验证;出现绿色对勾就代表通过。你也可以随时点上方按钮、用一组新的随机数重新验证。',
      referenceSource: {
        repo: '参考值实时取自公开仓库(变更历史可在 git 审计)',
        bakedIn: '参考值使用内置回退副本(仓库副本拉取失败,数值可能滞后)'
      }
    },
    auditors: {
      title: '给审计者',
      desc: '详细证据默认折叠,保持页面清爽——展开任一项即可逐行核对。',
      checks: '逐项检查结果',
      checksPassed: '{n} 项通过',
      checksFailed: '{n} 项失败',
      checksInfo: '{n} 项提示',
      rawReport: '原始证明数据',
      referenceHint: '逐行比对指纹'
    },
    reference: {
      title: '公布的参考值(供进阶核对)',
      desc: '这些是这台机密电脑的"精确指纹",来自公开仓库的 deploy/phala/reference.json 与 docs/attestation-verification.md。任何第三方都能用它们和实时读到的硬件证明逐条比对——数值一致,就说明跑的确实是这套公开、未被篡改的代码。',
      enclave: 'publicai-gateway — 机密电脑(单 enclave)',
      appId: '应用标识 (App ID)',
      osImage: '操作系统镜像',
      composeHash: '部署内容指纹 (compose_hash)',
      sourceCommit: '源码版本 (commit)',
      serviceImage: '服务镜像',
      attestorImage: '证明服务镜像',
      ciNote: '上述镜像由本仓库的 GitHub Actions 自动构建并签名(带来源证明,可公开核验):'
    }
  }
}
