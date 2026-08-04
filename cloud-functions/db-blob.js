// EdgeOne Makers Node 云函数：Blob 代理。
// @edgeone/pages-blob 仅有 Node SDK，Go 主程序（api.go）通过本函数
// 把 SQLite 快照存入 EdgeOne Blob，实现 DB_PERSIST=edgeone-blob。
// 路由（由 DB_PERSIST_EDGEONE_SECRET 校验）：
//   GET  /db-blob  下载数据库快照（404 表示尚无快照）
//   POST /db-blob  返回预签名上传 URL（云函数请求体上限 6MB，上传须直写 Blob）
import { getStore } from "@edgeone/pages-blob";

const DEFAULT_STORE = "cloudreve-db";
const DEFAULT_KEY = "cloudreve.db";
const CONTENT_TYPE = "application/octet-stream";

export async function onRequest({ request, env }) {
  const storeName = env?.DB_PERSIST_EDGEONE_STORE || DEFAULT_STORE;
  const key = env?.DB_PERSIST_EDGEONE_KEY || DEFAULT_KEY;
  const secret = env?.DB_PERSIST_EDGEONE_SECRET;

  const auth = request.headers.get("Authorization");
  if (!secret || auth !== `Bearer ${secret}`) {
    return Response.json({ error: "unauthorized" }, { status: 401 });
  }

  // getStore 在 Makers Functions 内自动创建命名空间，无需控制台配置；
  // 数据库恢复必须读最新数据，全程强一致。
  const store = getStore({ name: storeName, consistency: "strong" });

  if (request.method === "GET") {
    const stream = await store.get(key, { type: "stream" });
    if (stream === null) {
      console.log(`[db-blob] GET 快照不存在 ${storeName}/${key}`);
      return Response.json({ error: "not found" }, { status: 404 });
    }
    console.log(`[db-blob] GET 快照下载 ${storeName}/${key}`);
    return new Response(stream, {
      headers: { "Content-Type": CONTENT_TYPE },
    });
  }

  if (request.method === "POST") {
    const { url, expiresAt } = await store.createUploadUrl(key, {
      expireSeconds: 3600,
      contentType: CONTENT_TYPE,
    });
    console.log(`[db-blob] POST 预签名上传 URL ${storeName}/${key}`);
    return Response.json({ url, expiresAt });
  }

  return Response.json({ error: "method not allowed" }, { status: 405 });
}
