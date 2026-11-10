// Offset-based pagination stream for kuayle list endpoints.
// kuayle 列表端点的 offset 分页流。
//
// kuayle uses offset pagination with `page` and `per_page` query params.
// Responses are wrapped in `ListResponse<T>` with `has_more` to signal
// more pages. This module wraps that into an async `Stream`.
// kuayle 使用 offset 分页，query 参数为 `page` 和 `per_page`。
// 响应包装在 `ListResponse<T>` 中，用 `has_more` 标识是否有更多页。
// 本模块将其包装为异步 `Stream`。

use futures_core::Stream;
use serde::de::DeserializeOwned;
use serde::Serialize;
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

use crate::client::Client;
use crate::error::KuayleError;
use crate::types::common::ListResponse;

type PendingFetch<T> = Pin<Box<dyn Future<Output = Result<ListResponse<T>, KuayleError>> + Send>>;

/// A streaming iterator over paginated kuayle API results.
/// kuayle 分页 API 结果的流式迭代器。
pub struct PaginationStream<T> {
    client: Client,
    path: String,
    query_base: serde_json::Value,
    per_page: u32,
    current_page: u32,
    buffer: std::vec::IntoIter<T>,
    done: bool,
    error: Option<KuayleError>,
    pending_fetch: Option<PendingFetch<T>>,
}

impl<T: DeserializeOwned + Send + 'static> PaginationStream<T> {
    pub fn new(client: Client, path: String, query_base: serde_json::Value, per_page: u32) -> Self {
        PaginationStream {
            client,
            path,
            query_base,
            per_page: per_page.min(100),
            current_page: 1,
            buffer: Vec::new().into_iter(),
            done: false,
            error: None,
            pending_fetch: None,
        }
    }

    fn start_fetch(&mut self) {
        if self.done || self.error.is_some() {
            return;
        }

        let client = self.client.clone();
        let path = self.path.clone();
        let mut query = self.query_base.clone();
        let page = self.current_page;
        let per_page = self.per_page;

        if let Some(obj) = query.as_object_mut() {
            obj.insert("page".into(), serde_json::Value::Number(page.into()));
            obj.insert(
                "per_page".into(),
                serde_json::Value::Number(per_page.into()),
            );
        }

        // Build query string using url crate for proper encoding.
        // 使用 url crate 构建 query string，正确编码。
        let mut serializer = url::form_urlencoded::Serializer::new(String::new());
        if let Some(obj) = query.as_object() {
            for (k, v) in obj {
                match v {
                    serde_json::Value::String(s) => {
                        serializer.append_pair(k, s);
                    }
                    serde_json::Value::Number(n) => {
                        serializer.append_pair(k, &n.to_string());
                    }
                    serde_json::Value::Bool(b) => {
                        serializer.append_pair(k, &b.to_string());
                    }
                    _ => {}
                }
            }
        }
        let query_string = serializer.finish();
        let full_path = if query_string.is_empty() {
            path.clone()
        } else {
            format!("{path}?{query_string}")
        };

        let fut = async move { client.get(&full_path).await };

        self.pending_fetch = Some(Box::pin(fut));
    }
}

impl<T: DeserializeOwned + Send + 'static> Stream for PaginationStream<T> {
    type Item = Result<T, KuayleError>;

    fn poll_next(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        // SAFETY: We never move `self` out of the Pin; we only access fields mutably.
        // `dyn Future` stored as `Pin<Box<dyn Future>>` makes the struct `!Unpin`,
        // so `get_mut()` is unavailable — `get_unchecked_mut()` is the required pattern.
        // 安全：我们不会将 `self` 移出 Pin；只可变访问字段。
        // `dyn Future` 以 `Pin<Box<dyn Future>>` 存储使结构体变为 `!Unpin`，
        // 因此 `get_mut()` 不可用 — `get_unchecked_mut()` 是必要的模式。
        let this = unsafe { self.get_unchecked_mut() };

        // Return buffered error once.
        // 返回一次缓存的错误。
        if let Some(err) = this.error.take() {
            return Poll::Ready(Some(Err(err)));
        }

        // Yield buffered items.
        // 产出缓存的项目。
        if let Some(item) = this.buffer.next() {
            return Poll::Ready(Some(Ok(item)));
        }

        // Done.
        if this.done {
            return Poll::Ready(None);
        }

        // Start fetch if needed.
        if this.pending_fetch.is_none() {
            this.start_fetch();
        }

        // Poll pending fetch.
        if let Some(ref mut fut) = this.pending_fetch {
            match fut.as_mut().poll(cx) {
                Poll::Ready(Ok(response)) => {
                    this.pending_fetch = None;
                    let has_more = response.has_more;
                    this.current_page += 1;

                    // Defense: if has_more=true but data is empty, don't loop forever.
                    // 防御：若 has_more=true 但 data 为空，防止无限循环。
                    if has_more && response.data.is_empty() {
                        this.done = true;
                        this.error = Some(KuayleError::Api {
                            code: "PAGINATION_ERROR".into(),
                            message: "server returned has_more=true with empty page".into(),
                        });
                        return Poll::Ready(Some(Err(this.error.take().unwrap())));
                    }

                    this.done = !has_more;
                    this.buffer = response.data.into_iter();

                    if let Some(item) = this.buffer.next() {
                        Poll::Ready(Some(Ok(item)))
                    } else if this.done {
                        Poll::Ready(None)
                    } else {
                        cx.waker().wake_by_ref();
                        Poll::Pending
                    }
                }
                Poll::Ready(Err(e)) => {
                    this.pending_fetch = None;
                    this.done = true;
                    Poll::Ready(Some(Err(e)))
                }
                Poll::Pending => Poll::Pending,
            }
        } else {
            Poll::Pending
        }
    }
}

impl Client {
    /// Stream individual items from a paginated endpoint.
    /// 从分页端点流式迭代单个项目。
    pub fn paginate<T: DeserializeOwned + Send + 'static>(
        &self,
        path: &str,
        query: impl Serialize,
    ) -> impl Stream<Item = Result<T, KuayleError>> + '_ {
        let query_value = serde_json::to_value(&query)
            .unwrap_or_else(|_| serde_json::Value::Object(serde_json::Map::new()));

        PaginationStream::new(self.clone(), path.to_string(), query_value, 100)
    }

    /// Same as `paginate` but accepts a pre-built `serde_json::Value`.
    /// 与 `paginate` 相同但接受预构建的 `serde_json::Value`。
    ///
    /// Avoids lifetime constraints when the query is constructed inline.
    /// 当 query 是内联构造时避免生命周期约束。
    pub fn paginate_raw<T: DeserializeOwned + Send + 'static>(
        &self,
        path: &str,
        query: serde_json::Value,
    ) -> impl Stream<Item = Result<T, KuayleError>> + '_ {
        PaginationStream::new(self.clone(), path.to_string(), query, 100)
    }
}
