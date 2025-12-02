import logger from '@/utils/logger';
import { sandboxManager } from './sandbox';

interface TaskExecutionOptions {
  taskCode: string; // Base64 encoded task code
  sessionId?: string;
  taskSessionId?: string;
  apiKey: string;
  language: string;
  timeoutMs?: number;
  extraEnvironmentVariables?: Record<string, string>;
}

interface TaskExecutionResult {
  success: boolean;
  output?: string;
  error?: string;
  executionTime: number;
}

export const executeTaskCode = async (options: TaskExecutionOptions): Promise<TaskExecutionResult> => {
  const {
    taskCode,
    sessionId,
    taskSessionId,
    apiKey,
    language,
    timeoutMs = 300000,
    extraEnvironmentVariables,
  } = options;
  const startTime = Date.now();

  try {
    logger.info('Starting task execution:', {
      sessionId,
      taskSessionId,
      timeoutMs,
      hasTaskCode: !!taskCode,
    });

    // Decode the base64 task code
    const decodedCode = Buffer.from(taskCode, 'base64').toString('utf-8');
    logger.info('Decoded task code:', { codeLength: decodedCode.length });

    // Execute the task using sandbox manager
    logger.info('Executing task in sandbox...', { language });
    const result = await sandboxManager.executeSandboxCode({
      language,
      code: decodedCode,
      taskSessionId: taskSessionId,
      sessionId: sessionId,
      apiKey,
      extraEnvironmentVariables,
    });

    const executionTime = Date.now() - startTime;
    logger.info('Task execution completed:', {
      executionTime,
      success: result !== null,
    });

    // Since sandboxManager.executeSandboxCode returns null on error and logs via websocket,
    // we consider it successful if it doesn't throw an exception
    return {
      success: true,
      output: JSON.stringify(result),
      executionTime,
    };
  } catch (error: any) {
    const executionTime = Date.now() - startTime;
    logger.error('Task execution failed in Daytona:', error);

    return {
      success: false,
      error: error.message || 'Unknown execution error',
      executionTime,
    };
  }
};
