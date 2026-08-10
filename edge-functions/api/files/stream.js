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

const SEG = 4 * 1024 * 1024; // 单段 4MB，稳妥低于约 6MB 的平台上限

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
    return jsonResponse(502, '上游下载失败: ' + e);
  }

  // 上游报错（签名无效/已过期/文件不存在等）：原样透传错误响应
  if (first.status !== 206 && first.status !== 200) {
    const body = await first.text();
    return new Response(body, {
      status: first.status,
      headers: { 'Content-Type': first.headers.get('Content-Type') || 'application/json' },
    });
  }

  const contentType = first.headers.get('Content-Type') || 'application/octet-stream';

  // 上游忽略 Range 直接返回整文件（200）：文件较小，直接透传其流
  if (first.status === 200) {
    const headers = {
      'Content-Type': contentType,
      'Content-Disposition': disposition(name),
    };
    const len = first.headers.get('Content-Length');
    if (len) headers['Content-Length'] = len;
    return new Response(first.body, { status: 200, headers });
  }

  // 从 Content-Range（bytes 0-4194303/10080448）解析总大小
  let total = -1;
  const cr = first.headers.get('Content-Range');
  if (cr) {
    const m = cr.match(/\/(\d+)\s*$/);
    if (m) total = parseInt(m[1], 10);
  }

  // 拼流：先吐首段，再逐段拉取后续分段
  const stream = new ReadableStream({
    async start(controller) {
      try {
        await pipeBody(first.body, controller);
        let offset = SEG;
        while (total > 0 && offset < total) {
          const end = Math.min(offset + SEG - 1, total - 1);
          const resp = await fetch(baseProxy, { headers: { Range: `bytes=${offset}-${end}` } });
          if (resp.status !== 206 && resp.status !== 200) {
            controller.error(new Error('分段下载失败，状态 ' + resp.status));
            return;
          }
          await pipeBody(resp.body, controller);
          offset = end + 1;
        }
        controller.close();
      } catch (e) {
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

// 把一个响应体读完并逐块推入控制器
async function pipeBody(body, controller) {
  if (!body) return;
  const reader = body.getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) controller.enqueue(value);
  }
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
