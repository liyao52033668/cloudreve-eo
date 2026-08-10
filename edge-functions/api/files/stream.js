// 大文件流式下载中继。
//
// EdgeOne Makers 的 Go 云函数响应会被平台整体缓冲且存在约 6MB 上限，
// 超过的文件无法完整送达浏览器（表现为浏览器只收到一小段、进度卡死）。
// 本边缘函数（V8，支持 ReadableStream、无该缓冲限制）把浏览器的一次下载
// 拆成多个 Range 分段请求，逐段回调 Go 的 /api/files/proxy，再拼成一条流
// 返回浏览器，从而绕开 6MB 上限并提供原生下载进度。
//
// 鉴权不在此处做：浏览器带来的 policy/key/name/exp/sig 查询参数原样透传给
// Go 端，每个分段请求仍由 Go 的 ProxyDownload 校验 HMAC 签名，安全性不变。

// 单段 1MB：稳妥远低于平台约 6MB 的响应缓冲上限。实测中 4MB 段在部分
// 部署下仍可能被缓冲截断（表现为下载中途"网络错误"），1MB 留足余量。
const SEG = 1 * 1024 * 1024;

export async function onRequest(context) {
  const { request } = context;
  const url = new URL(request.url);
  const qs = url.searchParams;
  const name = qs.get('name') || '';

  // 回调本站 Go 云函数的代理端点（签名查询参数原样带上）
  const baseProxy = url.origin + '/api/files/proxy?' + qs.toString();

  // 首段请求：顺带通过 Content-Range 拿到文件总大小，用于设置 Content-Length
  let first;
  try {
    first = await fetch(baseProxy, { headers: { Range: `bytes=0-${SEG - 1}` } });
  } catch (e) {
    console.error('stream: 首段请求失败', String(e));
    return jsonResponse(502, '上游下载失败: ' + e);
  }

  // 上游报错（签名无效/已过期/文件不存在等）：原样透传错误响应
  if (first.status !== 206 && first.status !== 200) {
    const body = await first.text();
    console.warn(`stream: 上游错误 ${first.status}: ${body.slice(0, 200)}`);
    return new Response(body, {
      status: first.status,
      headers: { 'Content-Type': first.headers.get('Content-Type') || 'application/json' },
    });
  }

  const contentType = first.headers.get('Content-Type') || 'application/octet-stream';
  const len = first.headers.get('Content-Length');

  // 上游忽略 Range 返回整文件（200）：小文件直接透传。
  // 若 Content-Length 与实际字节不符（大文件被云函数缓冲截断），显式报错，
  // 避免浏览器收到长度不匹配的响应体而报"网络错误"。
  if (first.status === 200) {
    console.log(`stream: 200 透传 content-length=${len}`);
    const body = await first.arrayBuffer();
    if (len && body.byteLength !== Number(len)) {
      console.error(`stream: 200 响应不完整 content-length=${len} actual=${body.byteLength}`);
      return jsonResponse(502, `下载响应不完整（${body.byteLength}/${len} 字节）`);
    }
    return new Response(body, {
      status: 200,
      headers: {
        'Content-Type': contentType,
        'Content-Disposition': disposition(name),
        ...(len ? { 'Content-Length': len } : {}),
      },
    });
  }

  // 从 Content-Range（bytes 0-1048575/10080448）解析总大小
  let total = -1;
  const cr = first.headers.get('Content-Range');
  if (cr) {
    const m = cr.match(/\/(\d+)\s*$/);
    if (m) total = parseInt(m[1], 10);
  }
  console.log(`stream: 206 拼流 total=${total} 首段 content-length=${len}`);

  // 拼流：先吐首段，再逐段拉取后续分段。每段校验实际字节数，
  // 不足（被截断/中断）时明确报错而不是拼出长度不符的流。
  const stream = new ReadableStream({
    async start(controller) {
      let totalRead = 0;
      try {
        let n = await pipeBody(first.body, controller);
        totalRead += n;
        console.log(`stream: seg#0 +${n}B 累计${totalRead}/${total}`);
        let offset = SEG;
        let seg = 1;
        while (total > 0 && offset < total) {
          const end = Math.min(offset + SEG - 1, total - 1);
          const expect = end - offset + 1;
          const resp = await fetch(baseProxy, { headers: { Range: `bytes=${offset}-${end}` } });
          if (resp.status !== 206 && resp.status !== 200) {
            console.error(`stream: seg#${seg} 失败 status=${resp.status}`);
            controller.error(new Error('分段下载失败，状态 ' + resp.status));
            return;
          }
          n = await pipeBody(resp.body, controller);
          totalRead += n;
          if (n < expect) {
            // upstream-cr 显示上游（百度）实际返回的 Content-Range，
            // 用于判断是上游段限制还是下载中断。
            console.error(`stream: seg#${seg} 字节不足 实际=${n} 预期=${expect} upstream-cr=${resp.headers.get('Content-Range')}`);
            controller.error(new Error(`分段 ${seg} 数据不完整`));
            return;
          }
          console.log(`stream: seg#${seg} +${n}B 累计${totalRead}/${total}`);
          offset = end + 1;
          seg++;
        }
        if (total > 0 && totalRead !== total) {
          console.error(`stream: 拼流字节 ${totalRead} ≠ total ${total}`);
          controller.error(new Error('下载数据不完整'));
          return;
        }
        controller.close();
        console.log(`stream: 完成，共 ${totalRead} 字节`);
      } catch (e) {
        console.error('stream: 拼流异常', String(e));
        controller.error(e);
      }
    },
  });

  const headers = {
    'Content-Type': contentType,
    'Content-Disposition': disposition(name),
    'Accept-Ranges': 'bytes',
  };
  if (total > 0) headers['Content-Length'] = String(total);

  return new Response(stream, { status: 200, headers });
}

// 把一个响应体读完并逐块推入控制器，返回读取的总字节数
async function pipeBody(body, controller) {
  if (!body) return 0;
  const reader = body.getReader();
  let n = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) {
      controller.enqueue(value);
      n += value.byteLength;
    }
  }
  return n;
}

// name 为空表示内联预览（图片/视频），否则强制附件下载
function disposition(name) {
  if (!name) return 'inline';
  return `attachment; filename*=UTF-8''${encodeURIComponent(name)}`;
}

function jsonResponse(status, msg) {
  return new Response(JSON.stringify({ error: msg }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}
