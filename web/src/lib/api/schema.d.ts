/**
 * NTM API Schema Types
 *
 * This file is generated from the OpenAPI spec using:
 *   npm run gen:api
 *
 * Do not edit manually.
 */

export interface paths {
  "/api/v1/health": {
    get: {
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["HealthResponse"];
          };
        };
      };
    };
  };
  "/api/v1/sessions": {
    get: {
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SessionListResponse"];
          };
        };
      };
    };
    post: {
      requestBody: {
        content: {
          "application/json": components["schemas"]["CreateSessionRequest"];
        };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SessionResponse"];
          };
        };
      };
    };
  };
  "/api/v1/sessions/{sessionId}": {
    get: {
      parameters: {
        path: {
          sessionId: string;
        };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SessionResponse"];
          };
        };
      };
    };
    delete: {
      parameters: {
        path: {
          sessionId: string;
        };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["SuccessResponse"];
          };
        };
      };
    };
  };
  "/api/v1/deps": {
    get: {
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["DepsResponse"];
          };
        };
      };
    };
  };
  "/api/kernel/commands": {
    get: {
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["KernelListResponse"];
          };
        };
      };
    };
  };
}

export interface components {
  schemas: {
    SuccessResponse: {
      success: boolean;
      timestamp: string;
      request_id?: string;
    };
    ErrorResponse: {
      success: boolean;
      timestamp: string;
      request_id?: string;
      error: string;
      error_code: string;
      details?: unknown;
    };
    HealthResponse: {
      success: boolean;
      timestamp: string;
      status: string;
      version?: string;
    };
    SessionListResponse: {
      success: boolean;
      timestamp: string;
      sessions: Session[];
    };
    SessionResponse: {
      success: boolean;
      timestamp: string;
      session: Session;
    };
    Session: {
      name: string;
      created_at?: string;
      panes?: Pane[];
      tags?: string[];
    };
    Pane: {
      index: number;
      agent_type?: string;
      title?: string;
      working_dir?: string;
    };
    CreateSessionRequest: {
      name: string;
      tags?: string[];
    };
    DepsResponse: {
      success: boolean;
      timestamp: string;
      dependencies: Dependency[];
    };
    Dependency: {
      name: string;
      required: boolean;
      available: boolean;
      version?: string;
    };
    KernelListResponse: {
      success: boolean;
      timestamp: string;
      commands: KernelCommand[];
    };
    KernelCommand: {
      name: string;
      description: string;
      category: string;
    };
  };
}

export interface operations {}
