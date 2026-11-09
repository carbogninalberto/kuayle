// Wiremock integration tests for pagination stream.
// 分页流的 wiremock 集成测试。

use futures_util::StreamExt;
use kuayle_sdk::client::Client;
use serde::{Deserialize, Serialize};
use serde_json::json;
use url::Url;
use wiremock::matchers::{method, path, query_param};
use wiremock::{Mock, MockServer, ResponseTemplate};

/// Simple item type for pagination tests.
/// 用于分页测试的简单项目类型。
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
struct TestItem {
    id: u32,
    name: String,
}

async fn test_client(server: &MockServer) -> Client {
    let base_url = Url::parse(&server.uri()).unwrap();
    Client::new(base_url, "kuayle_pat_test".into())
}

fn paginated_response(
    items: Vec<TestItem>,
    page: u32,
    per_page: u32,
    total: u64,
) -> serde_json::Value {
    let has_more = (page as u64 * per_page as u64) < total;
    json!({
        "data": items,
        "total_count": total,
        "page": page,
        "per_page": per_page,
        "has_more": has_more
    })
}

#[tokio::test]
async fn paginate_single_page() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    let items = vec![
        TestItem {
            id: 1,
            name: "one".into(),
        },
        TestItem {
            id: 2,
            name: "two".into(),
        },
    ];

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "1"))
        .and(query_param("per_page", "100"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(
            items.clone(),
            1,
            100,
            2,
        )))
        .expect(1)
        .mount(&server)
        .await;

    let mut stream = client.paginate::<TestItem>("/api/issues", &json!({}));
    let mut results = Vec::new();
    while let Some(Ok(item)) = stream.next().await {
        results.push(item);
    }

    assert_eq!(results.len(), 2);
    assert_eq!(results[0].id, 1);
    assert_eq!(results[1].id, 2);
}

#[tokio::test]
async fn paginate_multiple_pages() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    let page1 = vec![TestItem {
        id: 1,
        name: "a".into(),
    }];
    let page2 = vec![TestItem {
        id: 2,
        name: "b".into(),
    }];
    let page3 = vec![TestItem {
        id: 3,
        name: "c".into(),
    }];

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "1"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(page1, 1, 1, 3)))
        .expect(1)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "2"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(page2, 2, 1, 3)))
        .expect(1)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "3"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(page3, 3, 1, 3)))
        .expect(1)
        .mount(&server)
        .await;

    let mut stream = client.paginate::<TestItem>("/api/issues", &json!({"per_page": 1}));
    let mut results = Vec::new();
    while let Some(Ok(item)) = stream.next().await {
        results.push(item);
    }

    assert_eq!(results.len(), 3);
    assert_eq!(results[0].id, 1);
    assert_eq!(results[1].id, 2);
    assert_eq!(results[2].id, 3);
}

#[tokio::test]
async fn paginate_empty() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "1"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(
            vec![],
            1,
            100,
            0,
        )))
        .expect(1)
        .mount(&server)
        .await;

    let mut stream = client.paginate::<TestItem>("/api/issues", &json!({}));
    let mut results = Vec::new();
    while let Some(Ok(item)) = stream.next().await {
        results.push(item);
    }

    assert!(results.is_empty());
}

#[tokio::test]
async fn paginate_error_on_first_page() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .respond_with(ResponseTemplate::new(401).set_body_json(json!({
            "error": {
                "code": "UNAUTHORIZED",
                "message": "bad token"
            }
        })))
        .expect(1)
        .mount(&server)
        .await;

    let mut stream = client.paginate::<TestItem>("/api/issues", &json!({}));
    let result = stream.next().await.unwrap();
    assert!(result.is_err());
}

#[tokio::test]
async fn paginate_error_after_first_page() {
    let server = MockServer::start().await;
    let client = test_client(&server).await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "1"))
        .respond_with(ResponseTemplate::new(200).set_body_json(paginated_response(
            vec![TestItem {
                id: 1,
                name: "a".into(),
            }],
            1,
            1,
            2,
        )))
        .expect(1)
        .mount(&server)
        .await;

    Mock::given(method("GET"))
        .and(path("/api/issues"))
        .and(query_param("page", "2"))
        .respond_with(ResponseTemplate::new(500).set_body_json(json!({
            "error": {
                "code": "INTERNAL_ERROR",
                "message": "boom"
            }
        })))
        .expect(1)
        .mount(&server)
        .await;

    let mut stream = client.paginate::<TestItem>("/api/issues", &json!({"per_page": 1}));
    let first = stream.next().await.unwrap();
    assert!(first.is_ok());

    let second = stream.next().await.unwrap();
    assert!(second.is_err());

    // Stream should not produce more items after error.
    // 错误后不应再产出更多项目。
    assert!(stream.next().await.is_none());
}
