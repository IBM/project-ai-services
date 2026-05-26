import litellm
from litellm.integrations.custom_logger import CustomLogger

class TokenizeTranslator(CustomLogger):
    def __init__(self):
        super().__init__()

    # 1. TRANSLATE REQUEST: vLLM -> Watsonx
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        # user_api_key_dict is a UserAPIKeyAuth object, access request_route as an attribute
        request_route = getattr(user_api_key_dict, "request_route", None) or ""
        print(f"PRE-API CALL: {request_route}")
        print(f"Original data: {data}")

        # Only apply this transformation if it looks like a vLLM tokenize request
        is_pass_through = call_type == "pass_through_endpoint"
        is_tokenize_route = request_route == "/tokenize"

        if is_pass_through and is_tokenize_route:
            if isinstance(data, dict) and "prompt" in data:
                # Translate to watsonx format
                prompt_string = data.pop("prompt")
                data["input"] = prompt_string
                model = litellm.os.environ.get("INSTRUCT_MODEL")
                data["model_id"] = model.split("/", 1)[1]
                data["project_id"] = litellm.os.environ.get("WATSONX_PROJECT_ID")
                data["parameters"] = {
                    "return_tokens": True
                } 
        print(f"Translated data: {data}")
        return data

    # 2. TRANSLATE RESPONSE: Watsonx -> vLLM
    async def async_post_call_success_hook(self, data, user_api_key_dict, response):
        # user_api_key_dict is a UserAPIKeyAuth object, access request_route as an attribute
        request_route = getattr(user_api_key_dict, "request_route", None) or ""
        print(f"POST-API CALL: {request_route}")
        print(f"Original response: {response}")

        # SAFEGUARD: Ensure this was specifically a pass-through request
        is_pass_through = data.get("call_type") == "pass_through_endpoint"
        is_tokenize_route = request_route == "/tokenize"
        
        if is_pass_through and is_tokenize_route:
            if isinstance(response, dict) and "result" in response:
                try:
                    # Extract IBM's token array and count directly from the 'result' object
                    tokens = response["result"]["tokens"]
                    token_count = response["result"].get("token_count", len(tokens))

                    # Overwrite the response dict with vLLM format
                    response.clear()
                    response["tokens"] = tokens
                    response["count"] = token_count
                except (KeyError, TypeError):
                    # If the schema still doesn't match perfectly, fail gracefully
                    pass
        print(f"Translated response: {response}")
        return response

# Instantiate it so the config can import it
translator_instance = TokenizeTranslator()
